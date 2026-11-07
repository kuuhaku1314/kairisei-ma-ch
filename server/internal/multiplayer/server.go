package multiplayer

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/wirecompression"
)

const (
	maxFrameBytes = 256 * 1024
	// Allow 500 active players plus a simultaneous reconnect wave. Pending
	// handshakes have a separate budget and cannot extend it with Ping.
	maxConnections         = 4096
	maxUnauthenticated     = 512
	connectionAuthLifetime = 30 * time.Second
	acceptRetryMin         = 5 * time.Millisecond
	acceptRetryMax         = time.Second
	acceptErrorLogInterval = 30 * time.Second
	// The CN client interprets RoomInfo.game_speed as a percentage and
	// multiplies it by 0.01 before assigning UnityEngine.Time.timeScale.
	DefaultGameSpeed     = 150
	roomCountdownSeconds = 3
)

type Server struct {
	hub             *Hub
	logger          *slog.Logger
	mu              sync.Mutex
	closed          bool
	conns           map[*clientConn]struct{}
	unauthenticated int // protected by mu
	listener        net.Listener
	stopping        chan struct{}
	workers         sync.WaitGroup
	downloadBytes   wirecompression.Metrics
}

type clientConn struct {
	server               *Server
	conn                 net.Conn
	authDeadline         time.Time // owned by serve; zero after room authentication
	writeMu              sync.Mutex
	lastDelivery         <-chan struct{} // protected by the room session, including terminal recovery
	deadlineMu           sync.Mutex
	finishingUntil       time.Time // protected by deadlineMu; ping cannot extend completion
	recoveryUntil        time.Time
	roomID               int64
	memberType           int
	userID               int
	recoveryVersion      int
	recoveryToken        string
	recoveryApplied      bool
	comebackPending      bool
	loadingComebackReady bool // connection gameplay flags are protected by the room session
	completedComeback    bool
	retired              bool
	closed               bool
	compression          frameCompression // written by serve under writeMu; read by writers under writeMu
}

func NewServer(hub *Hub, logger *slog.Logger) (*Server, error) {
	if hub == nil {
		return nil, errors.New("multiplayer hub is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{hub: hub, logger: logger, conns: make(map[*clientConn]struct{}), stopping: make(chan struct{})}, nil
}

func (s *Server) Serve(listener net.Listener) error {
	if listener == nil {
		return errors.New("battle listener is required")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = listener.Close()
		return nil
	}
	if s.listener != nil {
		s.mu.Unlock()
		return errors.New("BattleSv server is already serving")
	}
	s.listener = listener
	s.mu.Unlock()
	var retryDelay time.Duration
	var nextErrorLog time.Time
	for {
		connection, err := listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || errors.Is(err, net.ErrClosed) {
				return nil
			}
			var temporary net.Error
			if !errors.As(err, &temporary) || !temporary.Temporary() {
				return fmt.Errorf("accept BattleSv connection: %w", err)
			}
			if retryDelay == 0 {
				retryDelay = acceptRetryMin
			} else {
				retryDelay = min(2*retryDelay, acceptRetryMax)
			}
			now := time.Now()
			if !now.Before(nextErrorLog) {
				s.logger.Warn("BattleSv temporary accept failure; retrying", "error", err, "retry_after", retryDelay)
				nextErrorLog = now.Add(acceptErrorLogInterval)
			}
			timer := time.NewTimer(retryDelay)
			select {
			case <-s.stopping:
				timer.Stop()
				return nil
			case <-timer.C:
				continue
			}
		}
		retryDelay = 0
		client := &clientConn{server: s, conn: connection, authDeadline: time.Now().Add(connectionAuthLifetime)}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = connection.Close()
			return nil
		}
		if len(s.conns) >= maxConnections || s.unauthenticated >= maxUnauthenticated {
			s.mu.Unlock()
			_ = connection.Close()
			continue
		}
		s.conns[client] = struct{}{}
		s.unauthenticated++
		s.workers.Add(1)
		s.mu.Unlock()
		go func() { defer s.workers.Done(); client.serve() }()
	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.stopping)
	}
	listener := s.listener
	connections := make([]*clientConn, 0, len(s.conns))
	for connection := range s.conns {
		connections = append(connections, connection)
	}
	s.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	for _, connection := range connections {
		_ = connection.close(false)
	}
	s.workers.Wait()
	return nil
}

// Timed starts can debit SQLite outside a client handler. Admit and track them
// under the same lifecycle lock so Close cannot miss a committing operation.
func (s *Server) beginWork() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.workers.Add(1)
	return true
}

func (c *clientConn) serve() {
	defer func() {
		_ = c.close(true)
		c.server.mu.Lock()
		delete(c.server.conns, c)
		if !c.authDeadline.IsZero() {
			c.server.unauthenticated--
		}
		c.server.mu.Unlock()
	}()
	reader := bufio.NewReaderSize(c.conn, 32*1024)
	for {
		// Original TbpClient.PingInterval is 5 seconds. Leave ample room for
		// mobile stalls, but never retain a silent or partial-frame socket forever.
		deadline := c.readDeadline()
		if !c.authDeadline.IsZero() {
			if !time.Now().Before(c.authDeadline) {
				return
			}
			if c.authDeadline.Before(deadline) {
				deadline = c.authDeadline
			}
		}
		_ = c.conn.SetReadDeadline(deadline)
		method, payload, err := c.readFrame(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				c.server.logger.Warn("BattleSv client frame failed", "remote", c.conn.RemoteAddr().String(), "error", err)
			}
			return
		}
		if !c.authDeadline.IsZero() && !time.Now().Before(c.authDeadline) {
			return
		}
		err = c.handle(method, payload)
		// Room creation, entry and both comeback paths establish userID only
		// after checking their credentials. No room lock is held here.
		if !c.authDeadline.IsZero() && c.userID > 0 {
			c.server.mu.Lock()
			c.server.unauthenticated--
			c.authDeadline = time.Time{}
			c.server.mu.Unlock()
		}
		if err != nil {
			var pending *completionPendingError
			if errors.As(err, &pending) {
				c.server.logger.Error("multiplayer completion pending; retry scheduled", "room_id", c.roomID, "error", err)
				continue
			}
			c.server.logger.Warn("BattleSv client request rejected", "method", method, "remote", c.conn.RemoteAddr().String(), "error", err)
			return
		}
	}
}

func (c *clientConn) handle(method string, payload string) error {
	if method != "Ping" && method != "SpeedUpRate" {
		c.server.logger.Info(
			"BattleSv client request",
			"method", method,
			"room_id", c.roomID,
			"member_type", c.memberType,
			"user_id", c.userID,
		)
	}
	switch method {
	case "Ping":
		return c.handlePing(payload)
	case "SpeedUpRate":
		// This is a one-way clock-drift diagnostic emitted by TbpClient after
		// Pong sampling. It is not a room mutation and has no response method.
		return nil
	case "RoomCreateRequest":
		return c.handleCreate(payload)
	case "AIRoomCreateRequest":
		return c.handleAIRoomCreate(payload)
	case "RoomEnterRequest":
		return c.handleEnter(payload)
	case "Comeback":
		return c.handleComeback(payload)
	case "ReadyToComeback":
		return c.handleReadyToComeback(payload)
	case "ComebackApplied":
		return c.handleComebackApplied(payload)
	case "RoomLoadingFinish":
		return c.handleLoadingFinish()
	case "RoomLeaveRequest":
		session := c.server.hub.lockRoomSession(c.roomID)
		if session.owner != nil {
			c.retired = true
		}
		session.Unlock()
		return c.close(true)
	case "RoomMatchingConditionResetRequest":
		return c.handleRoomMatchingConditionReset(payload)
	case "Chat":
		return c.handleChat(payload, false)
	case "ChatNew":
		return c.handleChat(payload, true)
	case "OwnerNotLeave":
		return c.handleOwnerNotLeave(payload)
	case "Continue":
		return c.handleContinue(payload)
	case "ContinuePhaseFinish":
		return c.handleContinuePhaseFinish(payload)
	case "GameOverPhaseFinish":
		return c.handleGameOverPhaseFinish(payload)
	case "Retire":
		return c.handleRetire(payload)
	case "RoomCountdownStartRequest":
		return c.handleCountdownStart()
	case "RoomCountdownCancelRequest":
		return c.handleCountdownCancel()
	case "LoadingFinish":
		return c.handleBattleLoadingFinish()
	case "GameStartFinish", "TurnPhaseFinish", "UserAttackFinish", "ChaliceSphrExecUserPhaseFinish", "EnemyPhaseFinish", "ChaliceSphrExecEnemyPhaseFinish":
		return c.handlePresentationFinish(method)
	case "GameNextFinish":
		return c.handleGameNextFinish(payload)
	case "CardPlayPlan":
		return c.handleCardPlayPlan(payload)
	case "CardPlay":
		return c.handleCardPlay(payload, false)
	case "CardPlayTimeup":
		return c.handleCardPlay(payload, true)
	case "CardPlayAuto":
		return c.handleCardPlayAuto()
	case "BurstSkillExec":
		return c.handleBurstSkillExec(payload)
	case "ChaliceSphrReserve":
		return c.handleChaliceSphrReserve(payload)
	case "ChaliceSphrSkip":
		return c.handleChaliceSphrSkip(payload)
	case "AwakeSkip":
		return c.handleAwakeSkip(payload)
	default:
		return fmt.Errorf("unsupported BattleSv method %q", method)
	}
}

func (c *clientConn) handleRoomMatchingConditionReset(payload string) error {
	if payload != "" {
		return errors.New("RoomMatchingConditionResetRequest payload is not empty")
	}
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	connected := exists && c.memberType >= 1 && c.memberType == current.OwnerMemberType && current.connections[c.memberType] == c && current.State == RoomStateOpen
	if !connected {
		session.Unlock()
		return errors.New("RoomMatchingConditionResetRequest has no open room owner")
	}
	// TeamRoom exposes this for password/friend-only rooms. Its native empty
	// notification closes each peer's password display; admission uses this same
	// room state, so merely acknowledging the request leaves the room locked.
	current.HasPassword = false
	current.RoomType = 0
	if private, ok := current.BossGroup.(roomPrivate); ok {
		private.password = ""
		current.BossGroup = private
	}
	deliveries := reserveRoomFramesLocked(roomConnections(current), battleFrame{"RoomMatchingConditionReset", ""})
	session.Unlock()
	broadcastRoomFrames(c.server, c.roomID, deliveries)
	return nil
}

func (c *clientConn) handleChat(payload string, useNewFrame bool) error {
	fields := splitCSV(payload)
	if len(fields) != 1 {
		return fmt.Errorf("Chat has %d fields", len(fields))
	}
	messageID, err := parseRangeInt(fields[0], 0, 2147483647)
	if err != nil {
		return err
	}
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	var connections []*clientConn
	if exists && c.memberType >= 1 && current.connections[c.memberType] == c && !c.comebackPending {
		connections = roomConnections(current)
	} else if completed := c.completedInteractionLocked(session.completed, time.Now()); completed != nil {
		connections = completedInteractionPeers(completed, time.Now())
	} else {
		session.Unlock()
		return errors.New("Chat has no connected room member")
	}
	peers := make([]*clientConn, 0, len(connections))
	for _, connection := range connections {
		if connection != c {
			peers = append(peers, connection)
		}
	}
	memberType := c.memberType

	method := "MemberChat"
	if useNewFrame {
		method = "MemberChatNew"
	}
	response := strconv.Itoa(memberType) + "," + strconv.Itoa(messageID)
	deliveries := reserveRoomFramesLocked(peers, battleFrame{method, response})
	session.Unlock()
	broadcastRoomFrames(c.server, c.roomID, deliveries)
	return nil
}

func (c *clientConn) handleOwnerNotLeave(payload string) error {
	if payload != "" {
		return errors.New("OwnerNotLeave payload is not empty")
	}
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	valid := exists && current.State == RoomStateOpen && c.memberType == current.OwnerMemberType && current.connections[c.memberType] == c
	session.Unlock()
	if !valid {
		return errors.New("OwnerNotLeave has no open owner connection")
	}
	// CN TeamRoom sends this only after recent host input. The local server has
	// no idle-owner eviction timer, so validating the owner is the complete
	// state transition and no response frame is expected by BattlesvClient.
	return nil
}

func (c *clientConn) handleCreate(payload string) error {
	return c.handleCreateRequest(payload, "RoomCreateRequestResult", false)
}

func (c *clientConn) handleAIRoomCreate(payload string) error {
	fields := splitCSV(payload)
	if len(fields) != 5 {
		return c.writeRoomRequestRejected("AIRoomCreateRequestResult", fmt.Errorf("AIRoomCreateRequest has %d fields", len(fields)))
	}
	// AIRoomCreateRequest is the compact BattleSv form authorized by the HTTP
	// TeamBattleAIRoomCreate call. Reuse the ordinary typed room constructor;
	// the AI variant differs only in response/start orchestration.
	request := joinCSV(fields[0], fields[1], "0", "", fields[2], fields[2], "0", "0", "0", fields[3], fields[4], "android")
	return c.handleCreateRequest(request, "AIRoomCreateRequestResult", true)
}

func (c *clientConn) handleCreateRequest(payload string, responseMethod string, autoStart bool) error {
	fields := splitCSV(payload)
	if len(fields) != 12 {
		return c.writeRoomRequestRejected(responseMethod, fmt.Errorf("RoomCreateRequest has %d fields", len(fields)))
	}
	userID, err := parsePositiveInt(fields[0])
	if err != nil {
		return c.writeRoomRequestRejected(responseMethod, fmt.Errorf("RoomCreateRequest user is invalid: %w", err))
	}
	bossID, err := parsePositiveInt(fields[1])
	if err != nil {
		return c.writeRoomRequestRejected(responseMethod, fmt.Errorf("RoomCreateRequest boss is invalid: %w", err))
	}
	roomType, err := parseRangeInt(fields[2], 0, 1)
	if err != nil {
		return c.writeRoomRequestRejected(responseMethod, fmt.Errorf("RoomCreateRequest room type is invalid: %w", err))
	}
	arthurType, err := parseRangeInt(fields[4], 1, 4)
	if err != nil {
		return c.writeRoomRequestRejected(responseMethod, fmt.Errorf("RoomCreateRequest Arthur type is invalid: %w", err))
	}
	memberType, err := parseRangeInt(fields[5], 1, 4)
	if err != nil {
		return c.writeRoomRequestRejected(responseMethod, fmt.Errorf("RoomCreateRequest member type is invalid: %w", err))
	}
	token, signature := fields[9], fields[10]

	hub := c.server.hub
	hub.mu.Lock()
	rejectLocked := func(cause error) error {
		hub.mu.Unlock()
		return c.writeRoomRequestRejected(responseMethod, cause)
	}
	hub.prunePendingLocked(time.Now())
	pending, exists := hub.pending[token]
	if hub.maintenance || !exists || pending.Kind != pendingCreate || pending.Signature != signature {
		return rejectLocked(errors.New("RoomCreateRequest credential is invalid"))
	}
	delete(hub.pending, token)
	if pending.Spec.BossID != bossID || pending.Spec.RoomType != roomType || pending.Spec.Password != fields[3] || pending.Member.ArthurType != arthurType || memberType != pending.Member.ArthurType {
		return rejectLocked(errors.New("RoomCreateRequest does not match HTTP authorization"))
	}
	if pending.Member.UserID != userID {
		return rejectLocked(errors.New("RoomCreateRequest user does not match HTTP authorization"))
	}
	if pending.Spec.AutoStart != autoStart {
		return rejectLocked(errors.New("room start mode does not match HTTP authorization"))
	}
	speedSource := hub.gameSpeed
	if hub.creatingRooms == 0 {
		hub.creatingDone = make(chan struct{})
	}
	hub.creatingRooms++
	hub.mu.Unlock()
	defer func() {
		hub.mu.Lock()
		hub.creatingRooms--
		if hub.creatingRooms == 0 {
			close(hub.creatingDone)
			hub.creatingDone = nil
		}
		hub.mu.Unlock()
	}()
	member := cloneMember(pending.Member)
	member.UserID = userID
	member.ArthurType = arthurType
	member.JobType = arthurType
	// The native battle panel orders other members by their protocol slot.
	// Assign professions to fixed slots from creation; host authority comes
	// from OwnerMemberType, not from reserving slot one for the room owner.
	member.MemberType = memberType
	if autoStart {
		member.IsRoomLoading = 1
	}
	comebackToken, err := newComebackToken()
	if err != nil {
		return err
	}
	roomID, err := hub.reserveRoomID()
	if err != nil {
		return c.writeRoomRequestRejected(responseMethod, fmt.Errorf("reserve room identity: %w", err))
	}
	startMembers := pending.Spec.GameStartMemberNum
	if startMembers == 0 {
		startMembers = defaultMemberCount
	}
	if !autoStart {
		startMembers = max(defaultMemberCount, startMembers)
	}
	speed := pending.Spec.GameSpeed
	if speedSource != nil {
		if forced := speedSource(); forced != 0 {
			speed = forced
		}
	}
	if speed == 0 {
		speed = DefaultGameSpeed
	}
	created := &room{
		RoomSnapshot: RoomSnapshot{
			StageType:          pending.Spec.StageType,
			RoomID:             roomID,
			BossID:             bossID,
			BossGroupID:        pending.Spec.BossGroupID,
			RoomType:           roomType,
			HasPassword:        fields[3] != "",
			NeedDeckRank:       pending.Spec.NeedDeckRank,
			NeedHP:             pending.Spec.NeedHP,
			NeedFame:           pending.Spec.NeedFame,
			Comment:            pending.Spec.Comment,
			AllowToLeave:       pending.Spec.AllowToLeave,
			GameStartMemberNum: startMembers,
			OwnerMemberType:    member.MemberType,
			GameSpeed:          speed,
			SkipBattleMessages: pending.Spec.SkipBattleMessages,
			State:              RoomStateOpen,
			BossGroup:          roomPrivate{value: pending.Spec.BossGroup, password: fields[3]},
			Members:            []Member{member},
		},
		battlePointUse:       pending.Spec.BattlePointUse,
		continueAllowed:      pending.Spec.ContinueAllowed,
		battles:              append([]gamestate.TeamBattleReplayBattle(nil), roomSpecBattles(pending.Spec)...),
		dropLedgerVersion:    pending.Spec.DropLedgerVersion,
		dropPlan:             cloneDropPlan(pending.Spec.DropPlan),
		settlementPlan:       pending.Spec.SettlementPlan,
		fameRewardsSet:       pending.Spec.FameRewardsSet,
		scorePolicy:          gamestate.CloneTeamBattleScorePolicy(pending.Spec.ScorePolicy),
		fameRewards:          cloneFameRewards(pending.Spec.FameRewards),
		connections:          map[int]*clientConn{member.MemberType: c},
		reservations:         make(map[int]roomReservation),
		comebackTokens:       map[int]string{member.MemberType: comebackToken},
		disconnectedUntil:    make(map[int]time.Time),
		ownerFallbackParty:   append([]Member(nil), pending.Spec.OwnerFallbackParty...),
		enemyPartyID:         pending.Spec.EnemyPartyID,
		enemyType:            pending.Spec.EnemyType,
		costInitial:          pending.Spec.CostInitial,
		awakeTakeover:        pending.Spec.AwakeTakeover,
		holdMax:              pending.Spec.HoldMax,
		burstGaugeInitial:    pending.Spec.BurstGaugeInitial,
		seed:                 pending.Spec.Seed,
		drops:                append([]BattleDrop(nil), pending.Spec.Drops...),
		battleLoading:        make(map[int]bool),
		gameStartFinished:    make(map[int]bool),
		turnPhaseFinished:    make(map[int]bool),
		cardPlayPlans:        make(map[int]cardPlaySubmission),
		cardPlaySubmissions:  make(map[int]cardPlaySubmission),
		userAttackFinished:   make(map[int]bool),
		chaliceUserFinished:  make(map[int]bool),
		enemyPhaseFinished:   make(map[int]bool),
		chaliceEnemyFinished: make(map[int]bool),
	}
	for index := range created.ownerFallbackParty {
		created.ownerFallbackParty[index] = cloneMember(created.ownerFallbackParty[index])
	}
	if autoStart {
		if _, err := fillOwnerFallbackPartyLocked(created); err != nil {
			return err
		}
		created.State = RoomStateBattle
	}
	created.session = &roomSession{}
	created.session.mu.Lock()
	session := &lockedRoomSession{hub: hub, owner: created.session, room: created}
	defer session.Unlock()
	created.session.publish(created)
	hub.mu.Lock()
	if hub.maintenance {
		hub.mu.Unlock()
		session.Unlock()
		return c.writeRoomRequestRejected(responseMethod, ErrMaintenance)
	}
	c.roomID, c.memberType, c.userID = roomID, member.MemberType, userID
	hub.rooms[roomID] = created
	hub.mu.Unlock()

	frames := []battleFrame{{method: responseMethod, payload: roomInfoCSV(created.RoomSnapshot, comebackToken, true)}}
	for _, roomMember := range created.Members {
		frames = append(frames, battleFrame{method: "RoomMember", payload: memberCSV(roomMember)})
	}
	frames = append(frames, roomVacancyFrames(created)...)
	if autoStart {
		countdownPayload := roomCountdownPayload(created)
		frames = append(frames, battleFrame{method: "RoomCountdownFinish", payload: countdownPayload})
	}
	if err := c.writeInitialRoomFramesAndUnlock(session, frames); err != nil {
		_ = c.close(true)
		return err
	}
	c.server.logger.Info(
		"local multiplayer room created",
		"room_id", roomID,
		"boss_id", bossID,
		"owner_user_id", userID,
		"auto_start", autoStart,
	)
	return nil
}

func (c *clientConn) handleEnter(payload string) error {
	fields := splitCSV(payload)
	if len(fields) != 7 {
		return c.writeRoomRequestRejected("RoomEnterRequestResult", fmt.Errorf("RoomEnterRequest has %d fields", len(fields)))
	}
	userID, err := parsePositiveInt(fields[0])
	if err != nil {
		return c.writeRoomRequestRejected("RoomEnterRequestResult", fmt.Errorf("RoomEnterRequest user is invalid: %w", err))
	}
	roomID, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || roomID <= 0 {
		return c.writeRoomRequestRejected("RoomEnterRequestResult", errors.New("RoomEnterRequest room id is invalid"))
	}
	arthurType, err := parseRangeInt(fields[3], 1, 4)
	if err != nil {
		return c.writeRoomRequestRejected("RoomEnterRequestResult", fmt.Errorf("RoomEnterRequest Arthur type is invalid: %w", err))
	}
	token, signature := fields[4], fields[5]

	hub := c.server.hub
	hub.mu.Lock()
	rejectLocked := func(cause error) error {
		hub.mu.Unlock()
		return c.writeRoomRequestRejected("RoomEnterRequestResult", cause)
	}
	hub.prunePendingLocked(time.Now())
	pending, exists := hub.pending[token]
	if hub.maintenance || !exists || pending.Kind != pendingEnter || pending.RoomID != roomID || pending.Signature != signature {
		return rejectLocked(errors.New("RoomEnterRequest credential is invalid"))
	}
	if pending.Member.UserID != userID || pending.Member.ArthurType != arthurType {
		return rejectLocked(errors.New("RoomEnterRequest does not match HTTP authorization"))
	}
	delete(hub.pending, token)
	hub.mu.Unlock()
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	rejectLocked = func(cause error) error {
		session.Unlock()
		return c.writeRoomRequestRejected("RoomEnterRequestResult", cause)
	}
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateOpen {
		return rejectLocked(ErrRoomUnavailable)
	}
	if len(current.Members) >= maxRoomMembers {
		return rejectLocked(ErrRoomArthurUnavailable)
	}
	if current.HasPassword && current.password() != fields[2] {
		return rejectLocked(errors.New("RoomEnterRequest password is invalid"))
	}
	if err := validateRoomRequirements(current.RoomSnapshot, pending.Member); err != nil {
		return rejectLocked(err)
	}
	for _, existing := range current.Members {
		if existing.UserID == userID {
			return rejectLocked(ErrRoomUnavailable)
		}
		if existing.ArthurType == arthurType {
			return rejectLocked(ErrRoomArthurUnavailable)
		}
	}
	reservation, exists := current.reservations[arthurType]
	if !exists || reservation.UserID != userID || !reservation.ExpiresAt.After(time.Now()) {
		return rejectLocked(fmt.Errorf("%w: reservation expired or missing", ErrRoomUnavailable))
	}
	memberType := reservation.MemberType
	if memberType < 1 || memberType > maxRoomMembers || roomMemberTypeOccupied(current, memberType) {
		return rejectLocked(ErrRoomArthurUnavailable)
	}
	member := cloneMember(pending.Member)
	member.UserID = userID
	member.ArthurType = arthurType
	member.JobType = arthurType
	member.MemberType = memberType
	comebackToken, err := newComebackToken()
	if err != nil {
		return err
	}
	hub.mu.RLock()
	if hub.maintenance {
		hub.mu.RUnlock()
		return rejectLocked(ErrMaintenance)
	}
	current.Members = append(current.Members, member)
	delete(current.reservations, arthurType)
	current.connections[memberType] = c
	current.comebackTokens[memberType] = comebackToken
	c.roomID, c.memberType, c.userID = roomID, memberType, userID
	hub.mu.RUnlock()

	frames := []battleFrame{{method: "RoomEnterRequestResult", payload: roomInfoCSV(current.RoomSnapshot, comebackToken, false)}}
	for _, existing := range current.Members {
		frames = append(frames, battleFrame{method: "RoomMember", payload: memberCSV(existing)})
	}
	frames = append(frames, roomVacancyFrames(current)...)
	// A newcomer missed earlier reservation broadcasts. Send them after the
	// member snapshots, whose managed consumer clears the corresponding flag.
	frames = append(frames, roomReservationFrames(current, time.Now())...)
	peers := make([]*clientConn, 0, len(current.connections)-1)
	for existingType, connection := range current.connections {
		if existingType != memberType {
			peers = append(peers, connection)
		}
	}
	updates := []battleFrame{{"RoomMemberReserve", joinCSV(strconv.Itoa(memberType), "0")}, {"RoomMember", memberCSV(member)}}
	updates = append(updates, roomVacancyFrames(current)...)
	updates = append(updates, roomReservationFrames(current, time.Now())...)
	deliveries := reserveRoomFramesLocked(peers, updates...)
	initialErr := c.writeInitialRoomFramesAndUnlock(session, frames)
	// Deliver every reservation even when the new socket failed. Its later
	// disconnect notification must follow the membership update for peers.
	broadcastRoomFrames(c.server, roomID, deliveries)
	if initialErr != nil {
		_ = c.close(true)
		return initialErr
	}
	c.server.logger.Info("local multiplayer room joined", "room_id", roomID, "member_type", memberType, "user_id", userID)
	return nil
}

func (c *clientConn) handleLoadingFinish() error {
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || c.memberType == 0 || current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("RoomLoadingFinish has no active room")
	}
	for index := range current.Members {
		if current.Members[index].MemberType == c.memberType {
			current.Members[index].IsRoomLoading = 1
			c.server.logger.Info(
				"local multiplayer room member ready",
				"room_id", c.roomID,
				"member_type", c.memberType,
				"user_id", c.userID,
			)
			// Before TeamRoom exists, OnRoomMemberReserve only caches its data.
			// Ready proves the scene can now apply the pending-entry UI state.
			if current.State == RoomStateOpen {
				if frames := roomReservationFrames(current, time.Now()); len(frames) > 0 {
					delivery := c.reserveFramesLocked(frames)
					session.Unlock()
					return delivery.send()
				}
			}
			session.Unlock()
			return nil
		}
	}
	session.Unlock()
	return errors.New("RoomLoadingFinish member is unavailable")
}

func (c *clientConn) handleCountdownStart() error {
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || c.memberType != current.OwnerMemberType || current.connections[c.memberType] != c {
		session.Unlock()
		return errors.New("RoomCountdownStartRequest is not allowed")
	}
	if current.State == RoomStateCountdown {
		session.Unlock()
		return c.writeFrame("RoomCountdownStartRequestRes", "0")
	}
	if current.State != RoomStateOpen {
		session.Unlock()
		return c.writeFrame("RoomCountdownStartRequestRes", "1")
	}
	if len(current.connections) < max(defaultMemberCount, current.GameStartMemberNum) || !allMembersReady(current) ||
		len(roomReservationFrames(current, time.Now())) > 0 {
		session.Unlock()
		// Managed TeamRoom enters Wait only for flag 0. A rejected start must
		// return nonzero so the room remains interactive.
		return c.writeFrame("RoomCountdownStartRequestRes", "1")
	}
	fallbacks, err := fillOwnerFallbackPartyLocked(current)
	if err != nil {
		session.Unlock()
		c.server.logger.Info("multiplayer start needs configured owner decks or more players", "room_id", c.roomID, "error", err)
		return c.writeFrame("RoomCountdownStartRequestRes", "1")
	}
	current.State = RoomStateCountdown
	current.countdownChecking = true
	current.countdownDeadline = time.Now().Add(roomCountdownSeconds * time.Second)
	current.countdownFinishPending = false
	roomID := current.RoomID
	session.Unlock()
	// Check only at the owner's Start click. The account callback runs outside
	// room locks; the countdown remains cancellable and does not charge yet.
	if err := c.server.authorizeBattleStart(roomID, RoomStateCountdown, true); err != nil {
		c.server.logger.Info("multiplayer start check rejected", "room_id", roomID, "error", err)
		return c.writeFrame("RoomCountdownStartRequestRes", "1")
	}
	session = session.relock()
	defer session.Unlock()
	if session.room != current || current.State != RoomStateCountdown || current.connections[c.memberType] != c {
		session.Unlock()
		return c.writeFrame("RoomCountdownStartRequestRes", "1")
	}
	current.countdownChecking = false
	current.countdownDeadline = time.Now().Add(roomCountdownSeconds * time.Second)
	connections := append([]*clientConn(nil), roomConnections(current)...)
	frames := make([]battleFrame, 0, len(fallbacks)+1)
	for _, fallback := range fallbacks {
		frames = append(frames, battleFrame{"RoomMember", memberCSV(fallback)})
	}
	frames = append(frames, battleFrame{"RoomCountdownStart", strconv.Itoa(roomCountdownSeconds)})
	deliveries := make([]frameDelivery, 0, len(connections))
	for _, connection := range connections {
		peerFrames := frames
		if connection == c {
			peerFrames = append([]battleFrame{{"RoomCountdownStartRequestRes", "0"}}, frames...)
		}
		deliveries = append(deliveries, connection.reserveFramesLocked(peerFrames))
	}
	session.Unlock()
	broadcastRoomFrames(c.server, roomID, deliveries)
	c.server.logger.Info(
		"local multiplayer room countdown started",
		"room_id", roomID,
		"connected_members", len(connections),
		"fallback_members", len(fallbacks),
	)
	time.AfterFunc(roomCountdownSeconds*time.Second, func() {
		if !c.server.beginWork() {
			return
		}
		defer c.server.workers.Done()
		c.server.finishCountdown(roomID)
	})
	return nil
}

func (s *Server) finishCountdown(roomID int64) {
	if err := s.authorizeBattleStart(roomID, RoomStateCountdown, false); err != nil {
		s.logger.Warn("multiplayer start rejected", "room_id", roomID, "error", err)
		return
	}
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateCountdown || current.countdownChecking {
		session.Unlock()
		return
	}
	if time.Now().Before(current.countdownDeadline) {
		session.Unlock()
		return // A cancelled countdown's timer must not finish a newer countdown.
	}
	if current.countdownSyncs > 0 {
		current.countdownFinishPending = true
		session.Unlock()
		return
	}
	if (current.startCommitting || hub.startAuthorizer != nil) && !current.hostCostPaid {
		session.Unlock()
		return
	}
	current.State = RoomStateBattle
	current.battleLoading = make(map[int]bool, len(current.connections))
	connections := append([]*clientConn(nil), roomConnections(current)...)
	payload := roomCountdownPayload(current)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"RoomCountdownFinish", payload})
	finishStartCommitLocked(current)
	session.Unlock()

	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info("local multiplayer room entered battle loading", "room_id", roomID)
}

func (c *clientConn) handleBattleLoadingFinish() error {
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || c.memberType == 0 {
		session.Unlock()
		return errors.New("LoadingFinish has no active battle")
	}
	if current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("LoadingFinish member is unavailable")
	}
	current.battleLoading[c.memberType] = true
	roomID := current.RoomID
	session.Unlock()
	return c.server.tryStartLoadedBattle(roomID)
}

func (c *clientConn) handleGameStartFinish() error {
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.gameStarted || c.memberType == 0 {
		session.Unlock()
		return errors.New("GameStartFinish has no active battle")
	}
	if current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("GameStartFinish member is unavailable")
	}
	current.gameStartFinished[c.memberType] = true
	roomID := current.RoomID
	session.Unlock()
	return c.server.tryAdvanceGameStart(roomID)
}

func (c *clientConn) handleTurnPhaseFinish() error {
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.turnPhaseStarted || c.memberType == 0 {
		session.Unlock()
		return errors.New("TurnPhaseFinish has no active turn phase")
	}
	if current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("TurnPhaseFinish member is unavailable")
	}
	current.turnPhaseFinished[c.memberType] = true
	roomID := current.RoomID
	session.Unlock()
	return c.server.tryAdvanceTurnPhase(roomID)
}

func (c *clientConn) handleCardPlayPlan(payload string) error {
	submission, err := parseCardPlaySubmission(payload)
	if err != nil {
		return err
	}
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.userPhaseStarted || c.memberType == 0 {
		session.Unlock()
		return errors.New("CardPlayPlan has no active input phase")
	}
	if current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("CardPlayPlan member is unavailable")
	}
	if current.userAttackStarted {
		session.Unlock()
		return nil
	}
	if _, submitted := current.cardPlaySubmissions[c.memberType]; submitted {
		session.Unlock()
		return nil
	}
	result, err := cardPlayPlanResult(current.engine, c.memberType, submission)
	if err != nil {
		session.Unlock()
		return err
	}
	current.cardPlayPlans[c.memberType] = submission
	roomID := current.RoomID
	connections := append([]*clientConn(nil), roomConnections(current)...)
	var deliveries []frameDelivery
	if result != "" {
		deliveries = reserveRoomFramesLocked(connections, battleFrame{"ApiCardPlayPlanR", result})
	}
	session.Unlock()

	broadcastRoomFrames(c.server, roomID, deliveries)

	c.server.logger.Info(
		"local multiplayer card play plan broadcast",
		"room_id", roomID,
		"member_type", c.memberType,
		"selected_cards", selectedCardCount(submission),
		"card_types", submission.CardTypes,
		"targets", submission.Targets,
		"sphere_slot", submission.SphereSlot,
		"sphere_target", submission.SphereTarget,
	)
	return nil
}

func (c *clientConn) handleCardPlay(payload string, timedOut bool) error {
	submission, err := parseCardPlaySubmission(payload)
	if err != nil {
		return err
	}
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || c.memberType == 0 {
		session.Unlock()
		return errors.New("CardPlay has no active input phase")
	}
	if current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("CardPlay member is unavailable")
	}
	// The stock KO UI can still send its old timeout after the room has
	// advanced. Empty dead-member input is already automatic and must not
	// disconnect the spectator or acknowledge a later animation phase.
	if current.engine != nil && !current.engine.MemberAlive(c.memberType) && selectedActionCount(submission) == 0 {
		session.Unlock()
		return nil
	}
	if !current.userPhaseStarted {
		session.Unlock()
		return errors.New("CardPlay has no active input phase")
	}
	if current.userAttackStarted {
		session.Unlock()
		return nil
	}
	if current.engine == nil {
		session.Unlock()
		return errors.New("CardPlay Go battle engine is unavailable")
	}
	if _, submitted := current.cardPlaySubmissions[c.memberType]; submitted {
		// A repeated request cannot replace a confirmed human or CPU choice,
		// including a KO member whose native Submit produces no result rows.
		session.Unlock()
		return nil
	}
	var result string
	var resultCount int
	err = current.engine.WithCardSelections(func(preview *CardSelectionBatch) error {
		if timedOut && selectedActionCount(submission) == 0 {
			submission, err = npcCardSubmission(preview, c.memberType, current.enemyType)
			if err != nil {
				return err
			}
		}
		results, err := submitRoomCardPlay(preview, c.memberType, submission)
		if err != nil {
			return err
		}
		result, err = encodeOptionalBattleResults(results)
		resultCount = len(results)
		return err
	})
	if err != nil {
		session.Unlock()
		return err
	}
	// Submit is final when received; only UserAttack waits for the team.
	// Keep the authoritative selection and its client confirmation together.
	current.cardPlaySubmissions[c.memberType] = submission
	delete(current.cardPlayPlans, c.memberType)
	roomID := current.RoomID
	var deliveries []frameDelivery
	if result != "" {
		deliveries = reserveRoomFramesLocked(roomConnections(current), battleFrame{"ApiCardPlayR", result})
	}
	selectedCards := selectedCardCount(submission)
	c.server.logger.Info(
		"local multiplayer card play accepted",
		"room_id", roomID,
		"member_type", c.memberType,
		"selected_cards", selectedCards,
		"timed_out", timedOut,
		"result_rows", resultCount,
	)
	session.Unlock()
	broadcastRoomFrames(c.server, roomID, deliveries)
	return c.server.tryAdvanceCardPlay(roomID)
}

func (c *clientConn) handleCardPlayAuto() error {
	return c.handleCardPlay(strings.Repeat("0,", 11)+"0", true)
}

func (c *clientConn) handleBurstSkillExec(payload string) error {
	submission, err := parseBurstSkillSubmission(payload)
	if err != nil {
		return err
	}
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.userPhaseStarted || c.memberType == 0 {
		session.Unlock()
		return errors.New("BurstSkillExec has no active input phase")
	}
	if current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("BurstSkillExec member is unavailable")
	}
	if current.userAttackStarted {
		session.Unlock()
		return nil
	}
	if _, submitted := current.cardPlaySubmissions[c.memberType]; submitted {
		session.Unlock()
		return errors.New("BurstSkillExec member already submitted cards")
	}
	if current.engine == nil {
		session.Unlock()
		return errors.New("BurstSkillExec Go battle engine is unavailable")
	}
	results, err := current.engine.ExecuteBurst(c.memberType, submission)
	if err != nil {
		session.Unlock()
		return err
	}
	result, err := encodeBattleResults(results)
	if err != nil {
		session.Unlock()
		return err
	}
	roomID := current.RoomID
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiBurstSkillExecR", result})
	session.Unlock()

	broadcastRoomFrames(c.server, roomID, deliveries)
	c.server.logger.Info(
		"local multiplayer burst skill broadcast",
		"room_id", roomID,
		"member_type", c.memberType,
		"skill_slot", submission.SkillSlot,
		"target", submission.Target,
		"card_types", submission.CardTypes,
		"result_rows", len(results),
	)
	return nil
}

func (c *clientConn) handleChaliceSphrReserve(payload string) error {
	fields := splitCSV(payload)
	if len(fields) != 1 {
		return fmt.Errorf("ChaliceSphrReserve has %d fields", len(fields))
	}
	slot, err := parseRangeInt(fields[0], 0, deckSphereSlots)
	if err != nil {
		return fmt.Errorf("ChaliceSphrReserve slot is invalid: %w", err)
	}
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists && c.completedInteractionLocked(session.completed, time.Now()) != nil {
		// GameClose has already been queued and the original UI closes its
		// reservation window. Drain in-flight input without touching the result.
		session.Unlock()
		return nil
	}
	if !exists || current.State != RoomStateBattle || c.memberType < 1 || c.memberType > maxRoomMembers || current.engine == nil {
		session.Unlock()
		return errors.New("ChaliceSphrReserve has no active battle member")
	}
	if current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("ChaliceSphrReserve member is unavailable")
	}
	results := []BattleResult{current.reserveChaliceInput(c.memberType, slot)}
	result, err := encodeBattleResults(results)
	if err != nil {
		session.Unlock()
		return err
	}
	roomID := current.RoomID
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiChaliceSphrReserve", result})
	session.Unlock()

	broadcastRoomFrames(c.server, roomID, deliveries)
	return nil
}

func (c *clientConn) handleChaliceSphrSkip(payload string) error {
	if payload != "" {
		return errors.New("ChaliceSphrSkip payload is not empty")
	}
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists {
		completed := c.completedInteractionLocked(session.completed, time.Now())
		if completed != nil && completed.OwnerMemberType == c.memberType {
			// Drain an owner's in-flight skip after the original movie has ended;
			// do not broadcast another movie operation into the closed phase.
			session.Unlock()
			return nil
		}
	}
	if !exists || current.State != RoomStateBattle || c.memberType == 0 {
		session.Unlock()
		return errors.New("ChaliceSphrSkip has no active battle member")
	}
	// The original client only lets the room owner skip the shared movie.
	if current.OwnerMemberType != c.memberType || current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("ChaliceSphrSkip is not allowed")
	}
	roomID := current.RoomID
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ChaliceSphrSkipExec", ""})
	session.Unlock()
	broadcastRoomFrames(c.server, roomID, deliveries)
	return nil
}

func (c *clientConn) handleUserAttackFinish() error {
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.userAttackStarted || c.memberType == 0 {
		session.Unlock()
		return errors.New("UserAttackFinish has no active user attack")
	}
	if current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("UserAttackFinish member is unavailable")
	}
	current.userAttackFinished[c.memberType] = true
	roomID := current.RoomID
	session.Unlock()
	return c.server.tryAdvanceUserAttack(roomID)
}

func (c *clientConn) handleChaliceSphrExecUserPhaseFinish() error {
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.chaliceUserStarted || c.memberType == 0 {
		session.Unlock()
		return errors.New("ChaliceSphrExecUserPhaseFinish has no active chalice phase")
	}
	if current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("ChaliceSphrExecUserPhaseFinish member is unavailable")
	}
	current.chaliceUserFinished[c.memberType] = true
	roomID := current.RoomID
	session.Unlock()
	return c.server.tryAdvanceChaliceUser(roomID)
}

func (c *clientConn) handleEnemyPhaseFinish() error {
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.enemyPhaseStarted || c.memberType == 0 {
		session.Unlock()
		return errors.New("EnemyPhaseFinish has no active enemy phase")
	}
	if current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("EnemyPhaseFinish member is unavailable")
	}
	current.enemyPhaseFinished[c.memberType] = true
	roomID := current.RoomID
	session.Unlock()
	return c.server.tryAdvanceEnemyPhase(roomID)
}

func (c *clientConn) handleChaliceSphrExecEnemyPhaseFinish() error {
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.chaliceEnemyStarted || c.memberType == 0 {
		session.Unlock()
		return errors.New("ChaliceSphrExecEnemyPhaseFinish has no active chalice phase")
	}
	if current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("ChaliceSphrExecEnemyPhaseFinish member is unavailable")
	}
	current.chaliceEnemyFinished[c.memberType] = true
	roomID := current.RoomID
	session.Unlock()
	return c.server.tryAdvanceChaliceEnemy(roomID)
}

func (c *clientConn) handleRetire(payload string) error {
	if payload != "" {
		return errors.New("Retire payload must be empty")
	}
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	activeBattle := exists && current.State == RoomStateBattle && c.memberType != 0 && current.connections[c.memberType] == c
	if !activeBattle {
		session.Unlock()
		return errors.New("Retire has no active battle")
	}
	c.retired = true
	delete(current.comebackTokens, c.memberType)
	delete(current.pendingComebackTokens, c.memberType)
	delete(current.disconnectedUntil, c.memberType)
	session.Unlock()
	// Retire is one-way. Release participation as soon as it is received;
	// the client's subsequent socket close may be delayed in transit.
	c.server.logger.Info(
		"local multiplayer member retired",
		"room_id", c.roomID,
		"member_type", c.memberType,
		"user_id", c.userID,
	)
	return c.close(true)
}

// completeBattle consumes the room lease. Account locks and SQLite work are
// acquired only after releasing it; publication follows the successful commit.
func completeBattle(session *lockedRoomSession, current *room, now time.Time) error {
	hub := session.hub
	defer session.Unlock()
	if hub == nil || current == nil || current.closing || current.RoomID <= 0 || current.State != RoomStateBattle || current.engineBattleEnd == 0 {
		return errors.New("completed battle state is invalid")
	}
	if current.pendingCompletion == nil {
		pending, err := freezeRoomCompletion(current, now)
		if err != nil {
			return err
		}
		current.pendingCompletion = pending
	}
	completed := current.pendingCompletion
	done := make(chan struct{})
	current.completionDone = done
	session.Unlock()
	var commitErr error
	if len(completed.OnlineUserIDs) != 0 {
		if hub.completionCommit != nil {
			commitErr = hub.completionCommit(completed.CompletedBattle, completed.expiresAt)
		} else if hub.repository != nil {
			commitErr = errors.New("multiplayer account completion transaction is unavailable")
		}
	}
	locked := session.relock()
	defer locked.Unlock()
	defer close(done)
	current.completionDone = nil
	if commitErr != nil {
		current.completionFailures++
		return &completionPendingError{commitErr}
	}
	if locked.room != current {
		return nil
	}
	current.State = RoomStateClosed
	if current.completionRetry != nil {
		current.completionRetry.Stop()
		current.completionRetry = nil
	}
	completed.pendingComebackTokens = maps.Clone(current.pendingComebackTokens)
	completed.terminalExpiresAt = time.Now().Add(comebackLifetime)
	completed.comebackConnections = make(map[int]*clientConn, len(current.connections))
	for memberType, connection := range current.connections {
		completed.comebackConnections[memberType] = connection
		connection.setFinishingDeadline(completed.terminalExpiresAt)
		if token := current.comebackTokens[memberType]; token != "" {
			completed.comebackTokens[memberType] = token
			completed.disconnectedUntil[memberType] = completed.terminalExpiresAt
		}
	}
	hub.mu.Lock()
	hub.completed[current.RoomID] = completed
	delete(hub.rooms, current.RoomID)
	hub.pruneCompletedLocked(now)
	hub.mu.Unlock()
	return nil
}

func freezeRoomCompletion(current *room, now time.Time) (*completedBattle, error) {
	onlineUserIDs := make([]int, 0, len(current.connections))
	eligible := make(map[int]struct{}, len(current.connections))
	completedTokens := make(map[int]string)
	completedDeadlines := make(map[int]time.Time)
	completedConnections := make(map[int]*clientConn)
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		connection := current.connections[memberType]
		if connection == nil || connection.retired || connection.userID <= 0 {
			continue
		}
		if _, duplicate := eligible[connection.userID]; duplicate {
			return nil, errors.New("completed battle repeats a connected account")
		}
		onlineUserIDs = append(onlineUserIDs, connection.userID)
		eligible[connection.userID] = struct{}{}
		completedConnections[memberType] = connection
		// Completion is committed before terminal frames are written. Retain
		// active peers too: a final-frame disconnect still needs RoomComeback.
		// The live connection prevents another socket from taking this slot.
		if token := current.comebackTokens[memberType]; token != "" {
			completedTokens[memberType] = token
			completedDeadlines[memberType] = now.Add(comebackLifetime)
		}
	}
	for _, member := range current.Members {
		if member.UserID <= 0 {
			continue
		}
		deadline, disconnected := current.disconnectedUntil[member.MemberType]
		token := current.comebackTokens[member.MemberType]
		if !disconnected || !deadline.After(now) || token == "" {
			continue
		}
		if _, duplicate := eligible[member.UserID]; duplicate {
			continue
		}
		onlineUserIDs = append(onlineUserIDs, member.UserID)
		eligible[member.UserID] = struct{}{}
		completedTokens[member.MemberType] = token
		completedDeadlines[member.MemberType] = deadline
	}
	if len(eligible) == 0 {
		return nil, errors.New("completed battle has no connected account")
	}
	projection := CompletedBattle{
		SettlementPlan:  current.settlementPlan,
		StageType:       current.StageType,
		ScorePolicy:     current.scorePolicy,
		EndType:         current.engineBattleEnd,
		FameRewardsSet:  current.fameRewardsSet,
		FameRewards:     current.fameRewards,
		BattleIndex:     current.battleIndex,
		Progress:        current.progress,
		HostCostPaid:    current.hostCostPaid,
		RoomID:          current.RoomID,
		BossID:          current.BossID,
		BossGroupID:     current.BossGroupID,
		OwnerMemberType: current.OwnerMemberType,
		Members:         append([]Member(nil), current.Members...),
		OnlineUserIDs:   onlineUserIDs,
		CompletedAtUnix: now.Unix(),
	}
	if current.engine != nil {
		if current.scorePolicy != nil {
			projection.ScoreDamage = current.engine.ScoreDamage()
		}
		recordRoomWaveDrops(current)
		projection.Turns = current.engine.TotalTurns()
		projection.DropLedgerVersion = current.dropLedgerVersion
		projection.DestroyedEnemyBits = current.destroyedEnemyBits
		projection.EnemyDeadBits = make([]int, max(len(current.battles), current.battleIndex+1))
		for wave, outcome := range current.waveOutcomes {
			projection.EnemyDeadBits[wave] = outcome.DestroyedEnemyBits
		}
		projection.ReleasedDrops = current.releasedDrops
	}
	projection = cloneCompletedBattle(projection)
	expiresAt := now.Add(completedLifetime)
	canSettle := current.engineBattleEnd == 1 || current.engineBattleEnd == 4 || (current.scorePolicy != nil && current.engineBattleEnd == 2)
	if !canSettle {
		// Ordinary CN defeat returns through the failure/back-stack flow. Keep
		// only a short-lived terminal snapshot, never a victory reward receipt.
		projection.OnlineUserIDs = nil
		expiresAt = now.Add(comebackLifetime)
	}
	return &completedBattle{
		session: current.session, CompletedBattle: projection, expiresAt: expiresAt,
		comebackTokens: completedTokens, disconnectedUntil: completedDeadlines,
		comebackConnections: completedConnections, terminalEngine: current.engine,
		terminalBattleEndType: current.engineBattleEnd,
	}, nil
}

func parseCardPlaySubmission(payload string) (cardPlaySubmission, error) {
	fields := splitCSV(payload)
	if len(fields) != 12 {
		return cardPlaySubmission{}, fmt.Errorf("card play has %d fields", len(fields))
	}
	var submission cardPlaySubmission
	for index := 0; index < len(submission.CardTypes); index++ {
		cardType, err := strconv.Atoi(fields[index*2])
		if err == nil && !battlecore.IsHandCardInput(cardType) {
			err = errors.New("BattleSv value is not a hand card type")
		}
		if err != nil {
			return cardPlaySubmission{}, fmt.Errorf("card play card %d is invalid: %w", index, err)
		}
		target, err := parseRangeInt(fields[index*2+1], 0, 8)
		if err != nil {
			return cardPlaySubmission{}, fmt.Errorf("card play target %d is invalid: %w", index, err)
		}
		submission.CardTypes[index] = cardType
		submission.Targets[index] = target
	}
	sphereSlot, err := parseRangeInt(fields[10], 0, 10)
	if err != nil {
		return cardPlaySubmission{}, fmt.Errorf("card play sphere slot is invalid: %w", err)
	}
	sphereTarget, err := parseRangeInt(fields[11], 0, 8)
	if err != nil {
		return cardPlaySubmission{}, fmt.Errorf("card play sphere target is invalid: %w", err)
	}
	submission.SphereSlot = sphereSlot
	submission.SphereTarget = sphereTarget
	return submission, nil
}

func parseBurstSkillSubmission(payload string) (burstSkillSubmission, error) {
	fields := splitCSV(payload)
	if len(fields) != 7 {
		return burstSkillSubmission{}, fmt.Errorf("burst skill has %d fields", len(fields))
	}
	target, err := parseRangeInt(fields[0], 0, 8)
	if err != nil {
		return burstSkillSubmission{}, fmt.Errorf("burst skill target is invalid: %w", err)
	}
	submission := burstSkillSubmission{Target: target}
	for index := range submission.CardTypes {
		cardType, err := strconv.Atoi(fields[index+1])
		if err == nil && !battlecore.IsHandCardInput(cardType) {
			err = errors.New("BattleSv value is not a hand card type")
		}
		if err != nil {
			return burstSkillSubmission{}, fmt.Errorf("burst skill card %d is invalid: %w", index, err)
		}
		submission.CardTypes[index] = cardType
	}
	skillSlot, err := parseRangeInt(fields[6], 1, 3)
	if err != nil {
		return burstSkillSubmission{}, fmt.Errorf("burst skill slot is invalid: %w", err)
	}
	submission.SkillSlot = skillSlot
	return submission, nil
}

func selectedCardCount(submission cardPlaySubmission) int {
	return submission.CardCount()
}

func selectedActionCount(submission cardPlaySubmission) int {
	return submission.ActionCount()
}

func (c *clientConn) handleCountdownCancel() error {
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || c.memberType != current.OwnerMemberType || current.connections[c.memberType] != c {
		session.Unlock()
		return errors.New("RoomCountdownCancelRequest is not allowed")
	}
	if current.State == RoomStateOpen {
		session.Unlock()
		return nil
	}
	if current.State == RoomStateBattle || current.startCommitting {
		session.Unlock()
		return nil
	}
	if current.State != RoomStateCountdown {
		session.Unlock()
		return errors.New("RoomCountdownCancelRequest has no pending countdown")
	}
	frames := reopenCountdownFramesLocked(current)
	connections := append([]*clientConn(nil), roomConnections(current)...)
	deliveries := reserveRoomFramesLocked(connections, frames...)
	session.Unlock()
	broadcastRoomFrames(c.server, c.roomID, deliveries)
	return nil
}

func (c *clientConn) close(updateHub bool) error {
	c.writeMu.Lock()
	if c.closed {
		c.writeMu.Unlock()
		return nil
	}
	c.closed = true
	err := c.conn.Close()
	c.writeMu.Unlock()
	if !updateHub || c.roomID == 0 {
		return err
	}

	hub := c.server.hub
	var comebackExpiry time.Time
	var startLoadedRoomID int64
	var advanceBattleRoomID int64
	var dissolvedPeers []*clientConn
	var memberUpdatePeers []*clientConn
	var memberUpdates []Member
	var cancelCountdownFrames []battleFrame
	var completeCountdownSync bool
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if exists {
		// The socket is already closed. Wait without either socket or Hub lock
		// so a pending debit resolves before choosing lobby vs battle cleanup.
		for exists && (current.startDone != nil || current.continueDone != nil || current.completionDone != nil) {
			done := current.startDone
			if done == nil {
				done = current.continueDone
			}
			if done == nil {
				done = current.completionDone
			}
			session.Unlock()
			<-done
			session = hub.lockRoomSession(c.roomID)
			defer session.Unlock()
			current, exists = session.room, session.room != nil
		}
	}
	if exists {
		if current.State == RoomStateBattle {
			// A stale socket must never remove a newer connection that has already
			// reclaimed the same member slot.
			if current.connections[c.memberType] == c {
				delete(current.connections, c.memberType)
				delete(current.battleLoading, c.memberType)
				delete(current.gameStartFinished, c.memberType)
				delete(current.gameNextFinished, c.memberType)
				delete(current.turnPhaseFinished, c.memberType)
				delete(current.cardPlayPlans, c.memberType)
				if current.engine == nil {
					delete(current.cardPlaySubmissions, c.memberType)
				} else if !current.engine.HasSubmission(c.memberType) {
					delete(current.cardPlaySubmissions, c.memberType)
				}
				delete(current.userAttackFinished, c.memberType)
				delete(current.chaliceUserFinished, c.memberType)
				delete(current.enemyPhaseFinished, c.memberType)
				delete(current.chaliceEnemyFinished, c.memberType)
				if current.continuation != nil {
					delete(current.continuation.finished, c.memberType)
					delete(current.continuation.declined, c.memberType)
				}
				if c.retired {
					delete(current.disconnectedUntil, c.memberType)
					delete(current.comebackTokens, c.memberType)
					delete(current.pendingComebackTokens, c.memberType)
				} else {
					comebackExpiry = time.Now().Add(comebackLifetime)
					current.disconnectedUntil[c.memberType] = comebackExpiry
				}
				// AI never owns a room. Comeback reservations matter only while
				// another human connection remains to keep the battle alive.
				if len(current.connections) == 0 && current.pendingCompletion == nil {
					hub.removeRoom(current)
				} else if !current.gameStarted && len(current.connections) > 0 &&
					len(current.battleLoading) == len(current.connections) {
					startLoadedRoomID = current.RoomID
				} else if current.gameStarted && len(current.connections) > 0 {
					advanceBattleRoomID = current.RoomID
				}
				c.server.logger.Info(
					"local multiplayer battle member became offline CPU",
					"room_id", c.roomID,
					"member_type", c.memberType,
					"remaining_connections", len(current.connections),
					"comeback_allowed", !c.retired,
				)
			}
		} else if c.memberType == current.OwnerMemberType {
			hub.removeRoom(current)
			for memberType, connection := range current.connections {
				if memberType == c.memberType {
					continue
				}
				dissolvedPeers = append(dissolvedPeers, connection)
			}
			c.server.logger.Info("local multiplayer room dissolved", "room_id", c.roomID)
		} else {
			wasCountdown := current.State == RoomStateCountdown
			removeMemberLocked(current, c.memberType)
			if wasCountdown && len(current.connections) < max(defaultMemberCount, current.GameStartMemberNum) {
				cancelCountdownFrames = reopenCountdownFramesLocked(current)
			} else if wasCountdown {
				fallbacks, fallbackErr := fillOwnerFallbackPartyLocked(current)
				if fallbackErr != nil {
					// The departing human may cover a profession for which the owner
					// has no playable deck. Return to the lobby without charging BP.
					cancelCountdownFrames = reopenCountdownFramesLocked(current)
					c.server.logger.Error(
						"replace disconnected countdown member failed",
						"room_id", c.roomID,
						"member_type", c.memberType,
						"error", fallbackErr,
					)
				} else {
					memberUpdates = append(memberUpdates, fallbacks...)
					current.countdownSyncs++
					completeCountdownSync = true
					c.server.logger.Info(
						"local multiplayer countdown member replaced by owner AI",
						"room_id", c.roomID,
						"member_type", c.memberType,
						"fallback_members", len(fallbacks),
					)
				}
			}
			if len(memberUpdates) == 0 {
				// Cancellation already publishes vacancies before reopening.
				if len(cancelCountdownFrames) == 0 {
					cancelCountdownFrames = append(roomVacancyFrames(current), roomReservationFrames(current, time.Now())...)
				}
			}
			memberUpdatePeers = append(memberUpdatePeers, roomConnections(current)...)
			c.server.logger.Info("local multiplayer room member left", "room_id", c.roomID, "member_type", c.memberType)
		}
	} else {
		// Also detach an original battle socket that disconnected after the
		// room moved to completed; it may not have received ApiGameEnd yet.
		if completed := session.completed; completed != nil &&
			completed.comebackConnections[c.memberType] == c {
			delete(completed.comebackConnections, c.memberType)
		}
	}
	deliveries := reserveRoomFramesLocked(dissolvedPeers, battleFrame{"RoomDelete", ""})
	frames := make([]battleFrame, 0, len(memberUpdates)+1)
	for _, member := range memberUpdates {
		frames = append(frames, battleFrame{"RoomMember", memberCSV(member)})
	}
	frames = append(frames, cancelCountdownFrames...)
	deliveries = append(deliveries, reserveRoomFramesLocked(memberUpdatePeers, frames...)...)
	session.Unlock()
	broadcastRoomFrames(c.server, c.roomID, deliveries)
	for _, connection := range dissolvedPeers {
		_ = connection.close(false)
	}
	if completeCountdownSync {
		c.server.completeCountdownMemberSync(c.roomID)
	}
	if !comebackExpiry.IsZero() {
		c.server.scheduleComebackExpiry(c.roomID, c.memberType, comebackExpiry)
	}
	if startLoadedRoomID != 0 {
		if startErr := c.server.tryStartLoadedBattle(startLoadedRoomID); startErr != nil {
			c.server.logger.Error("start battle after loading member disconnect failed", "room_id", startLoadedRoomID, "error", startErr)
		}
	}
	if advanceBattleRoomID != 0 {
		if advanceErr := c.server.advanceBattleAfterDisconnect(advanceBattleRoomID); advanceErr != nil {
			c.server.logger.Error("advance battle after member disconnect failed", "room_id", advanceBattleRoomID, "error", advanceErr)
		}
	}
	return err
}

func (s *Server) completeCountdownMemberSync(roomID int64) {
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.countdownSyncs <= 0 {
		session.Unlock()
		return
	}
	current.countdownSyncs--
	finishPending := current.countdownSyncs == 0 && current.countdownFinishPending && current.State == RoomStateCountdown
	if finishPending {
		current.countdownFinishPending = false
	}
	session.Unlock()
	if finishPending {
		s.finishCountdown(roomID)
	}
}

func (c *clientConn) writeFrame(method string, payload string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrameLocked(method, payload)
}

// writeFrameLocked requires writeMu, but never the global Hub lock.
func (c *clientConn) writeFrameLocked(method string, payload string) error {
	return c.writePreparedFrameLocked(&preparedBattleFrame{frame: battleFrame{method, payload}})
}

func readFrame(reader *bufio.Reader) (string, string, error) {
	header, err := readBoundedFrameLine(reader, maxFrameBytes)
	if err != nil {
		return "", "", err
	}
	if len(header) > maxFrameBytes || !strings.HasSuffix(header, "{\n") {
		return "", "", errors.New("BattleSv frame header is invalid")
	}
	method := strings.TrimSuffix(header, "{\n")
	if method == "" || strings.ContainsAny(method, "{}\r\n") {
		return "", "", errors.New("BattleSv frame method is invalid")
	}
	var lines []string
	total := len(header)
	for {
		line, readErr := readBoundedFrameLine(reader, maxFrameBytes-total)
		if readErr != nil {
			return "", "", readErr
		}
		total += len(line)
		if total > maxFrameBytes {
			return "", "", errors.New("BattleSv frame is too large")
		}
		if line == "}\n" {
			return method, strings.Join(lines, "\n"), nil
		}
		lines = append(lines, strings.TrimSuffix(line, "\n"))
	}
}

// ReadString allocates until a newline before the caller can check its size.
// ReadSlice lets us reject a malicious unterminated line at the frame budget.
func readBoundedFrameLine(reader *bufio.Reader, remaining int) (string, error) {
	var line strings.Builder
	for {
		part, err := reader.ReadSlice('\n')
		if len(part) > remaining-line.Len() {
			return "", errors.New("BattleSv frame is too large")
		}
		line.Write(part)
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line.String(), err
		}
	}
}

func splitCSV(payload string) []string {
	if payload == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(payload, "\n"), ",")
}

func joinCSV(fields ...string) string {
	for index := range fields {
		fields[index] = sanitizeCSV(fields[index])
	}
	return strings.Join(fields, ",")
}

func sanitizeCSV(value string) string {
	value = strings.ReplaceAll(value, ",", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}

func memberCSV(member Member) string {
	// TeamRoom.SetupMember uses is_cpu, rather than userid, to remove the
	// departed model. A vacant lobby slot must not enter its human-load path.
	isCPU := "0"
	if member.UserID == 0 {
		isCPU = "1"
	}
	fields := []string{
		strconv.Itoa(member.MemberType),
		strconv.Itoa(member.UserID),
		strconv.Itoa(member.Level),
		strconv.Itoa(member.ArthurType),
		strconv.Itoa(member.JobType),
		strconv.Itoa(member.HP),
		strconv.Itoa(member.Attack),
		strconv.Itoa(member.Magic),
		strconv.Itoa(member.Mind),
		isCPU,
		sanitizeCSV(member.Name),
		strconv.Itoa(member.LeaderCardID),
		strconv.Itoa(member.LeaderFame),
		strconv.Itoa(member.LeaderLevel),
		strconv.Itoa(member.DeckRank),
		strconv.Itoa(member.IsRoomLoading),
		strconv.Itoa(member.IsFirstMatch),
		strconv.Itoa(member.CostumeID),
	}
	for index := 0; index < 7; index++ {
		value := 0
		if index < len(member.PartsIDs) {
			value = member.PartsIDs[index]
		}
		fields = append(fields, strconv.Itoa(value))
	}
	fields = append(fields, strconv.Itoa(member.RookieType))
	for index := 0; index < 4; index++ {
		value := 0
		if index < len(member.DeckHonorIDs) {
			value = member.DeckHonorIDs[index]
		}
		fields = append(fields, strconv.Itoa(value))
	}
	fields = append(fields, strconv.Itoa(member.IsBuddy))
	return strings.Join(fields, ",")
}

func parsePositiveInt(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, errors.New("BattleSv positive integer is invalid")
	}
	return parsed, nil
}

func parseRangeInt(value string, minimum int, maximum int) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, errors.New("BattleSv integer is outside its range")
	}
	return parsed, nil
}

func roomMemberTypeOccupied(current *room, memberType int) bool {
	if current == nil || memberType < 1 || memberType > maxRoomMembers {
		return false
	}
	for _, member := range current.Members {
		if member.MemberType == memberType {
			return true
		}
	}
	return false
}

func removeMemberLocked(current *room, memberType int) {
	for index, member := range current.Members {
		if member.MemberType == memberType {
			current.Members = append(current.Members[:index], current.Members[index+1:]...)
			break
		}
	}
	delete(current.connections, memberType)
	delete(current.comebackTokens, memberType)
	delete(current.pendingComebackTokens, memberType)
	delete(current.disconnectedUntil, memberType)
}

// Managed TeamRoom only calls RoomIn (which shows the waiting label) for
// non-null RoomMemberInfo. Empty seats are display records, not room occupants.
// Human entry, CPU filling and vacancy display share this assignment. Never
// replace a waiting Arthur with a different profession merely because that
// player joined first: the CN portrait loader completes asynchronously.
func roomProfessionSlots(current *room) [5]int {
	var slots [5]int
	used := make(map[int]bool, maxRoomMembers)
	for _, member := range current.Members {
		slots[member.MemberType] = member.ArthurType
		used[member.ArthurType] = true
	}
	arthurType := 1
	for seat := 1; seat <= maxRoomMembers; seat++ {
		if slots[seat] != 0 {
			continue
		}
		for used[arthurType] {
			arthurType++
		}
		slots[seat] = arthurType
		arthurType++
	}
	return slots
}

func roomVacancyFrames(current *room) []battleFrame {
	var frames []battleFrame
	for seat, arthurType := range roomProfessionSlots(current) {
		if seat == 0 || roomMemberTypeOccupied(current, seat) {
			continue
		}
		vacancy := Member{MemberType: seat, Level: 1, ArthurType: arthurType, JobType: arthurType, HP: 1,
			LeaderCardID: 1, LeaderFame: 1, LeaderLevel: 1}
		frames = append(frames, battleFrame{"RoomMember", memberCSV(vacancy)})
	}
	return frames
}

// Countdown-only fallback characters occupy battle slots, not lobby seats.
// Publish their vacancies before cancellation makes the client interactive.
func reopenCountdownFramesLocked(current *room) []battleFrame {
	current.State = RoomStateOpen
	current.countdownChecking = false
	current.countdownFinishPending = false
	for index := 0; index < len(current.Members); {
		memberType := current.Members[index].MemberType
		if memberType != current.OwnerMemberType && current.connections[memberType] == nil {
			removeMemberLocked(current, memberType)
			continue
		}
		index++
	}
	frames := append(roomVacancyFrames(current), roomReservationFrames(current, time.Now())...)
	return append(frames, battleFrame{"RoomCountdownCancel", ""})
}

func allMembersReady(current *room) bool {
	for _, member := range current.Members {
		if member.IsRoomLoading == 0 {
			return false
		}
	}
	return true
}

func fillOwnerFallbackPartyLocked(current *room) ([]Member, error) {
	// Stage additions so a missing later profession cannot leave phantom CPUs
	// in an otherwise open room after rejecting its countdown.
	staged := &room{RoomSnapshot: RoomSnapshot{Members: append([]Member(nil), current.Members...)}}
	usedArthurTypes := make(map[int]struct{}, len(current.Members))
	for _, member := range current.Members {
		usedArthurTypes[member.ArthurType] = struct{}{}
	}
	added := make([]Member, 0, maxRoomMembers-len(current.Members))
	for _, candidate := range current.ownerFallbackParty {
		if _, exists := usedArthurTypes[candidate.ArthurType]; exists {
			continue
		}
		memberType := 0
		for seat, arthurType := range roomProfessionSlots(staged) {
			if arthurType == candidate.ArthurType && !roomMemberTypeOccupied(staged, seat) {
				memberType = seat
				break
			}
		}
		if memberType == 0 {
			return nil, errors.New("room fallback party has no free member slot")
		}
		candidate = cloneMember(candidate)
		candidate.MemberType = memberType
		candidate.IsRoomLoading = 1
		staged.Members = append(staged.Members, candidate)
		usedArthurTypes[candidate.ArthurType] = struct{}{}
		added = append(added, cloneMember(candidate))
	}
	if len(staged.Members) != maxRoomMembers || len(usedArthurTypes) != maxRoomMembers {
		return nil, errors.New("room fallback party did not close all four Arthur roles")
	}
	current.Members = staged.Members
	return added, nil
}

// Pending comeback members still participate in room barriers. Live directions
// become deliverable only after their authoritative snapshot has been reserved.
func roomConnections(current *room) []*clientConn {
	result := make([]*clientConn, 0, len(current.connections))
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		if connection := current.connections[memberType]; connection != nil && !connection.comebackPending {
			result = append(result, connection)
		}
	}
	return result
}
