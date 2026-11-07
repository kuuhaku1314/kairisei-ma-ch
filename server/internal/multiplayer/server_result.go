package multiplayer

import (
	"errors"
	"strconv"
)

const localRoomRequestRejected = "-1"

// Keep the established CN room prefix. Its original parser ignores trailing
// columns; the modern adapter consumes the added room-owned presentation flag.
// JP's equivalent field is before its comeback token, so wire position and
// the shared RoomInfo meaning are deliberately separate here.
func roomInfoCSV(room RoomSnapshot, comebackToken string, creating bool) string {
	fields := []string{"0", "", strconv.FormatInt(room.RoomID, 10), strconv.Itoa(room.BossID),
		strconv.Itoa(room.OwnerMemberType), strconv.Itoa(room.GameSpeed), comebackToken}
	if creating {
		fields = append(fields, strconv.Itoa(room.GameStartMemberNum))
	}
	skip := "0"
	if room.SkipBattleMessages {
		skip = "1"
	}
	return joinCSV(append(fields, skip, strconv.Itoa(recoveryProtocolVersion))...)
}

func (c *clientConn) writeRoomRequestRejected(method string, cause error) error {
	if method == "" {
		return errors.New("room request result method is empty")
	}
	message := "local room request rejected"
	if cause != nil && cause.Error() != "" {
		message = cause.Error()
	}
	c.server.logger.Info(
		"local multiplayer room request rejected",
		"method", method,
		"room_id", c.roomID,
		"member_type", c.memberType,
		"user_id", c.userID,
		"reason", message,
	)
	code := localRoomRequestRejected
	if method == "RoomEnterRequestResult" {
		if errors.Is(cause, ErrRoomArthurUnavailable) {
			code = "-3202" // CN TEAMBATTLE_ROOM_ARTHUR_TYPE_FILL.
		} else if errors.Is(cause, ErrRoomUnavailable) {
			code = "-3208" // CN TEAMBATTLE_ROOM_NOT_FOUND.
		}
	}
	// The original consumer uses RoomInfo only on success. A failed request
	// needs just res_code,res_str, without a fake room object.
	return c.writeFrame(method, joinCSV(code, message))
}
