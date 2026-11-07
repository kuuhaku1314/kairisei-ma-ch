package admin

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // cover uploads accept JPEG
	_ "image/png"  // and PNG, the formats the client texture loader reads
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
)

// Operator pools own their rules and publication group. Deletion is soft so
// play counts, audit history and identities survive and the pool can be restored.
const (
	customGachaKey     = "gacha-custom"
	customGachaFirstID = gamestate.OperatorGachaFirstID
	gachaCoverFolder   = "gacha-covers"
	gachaCoverMaxBytes = 2 * 1024 * 1024
)

var gachaCoverPathPattern = regexp.MustCompile(`^gacha-covers/[0-9a-f]{64}\.(png|jpg)$`)

// A 4096x4096 PNG can expand into a large pixel buffer. Validate one stream at a
// time; compressed request size alone does not bound concurrent decode memory.
var gachaCoverDecodeSlot = make(chan struct{}, 1)

type customGachaPool struct {
	GachaID     int            `json:"gacha_id"`
	GroupID     int            `json:"group_id"`
	TemplateID  int            `json:"template_id,omitempty"` // Read existing template-based pools only.
	Name        string         `json:"name"`
	Deleted     bool           `json:"deleted,omitempty"`
	RuleVersion int            `json:"rule_version,omitempty"` // 0: legacy copy; 1: complete built-in rule template.
	ArthurType  int8           `json:"arthur_type,omitempty"`  // Native profession picker: 1–4 professions, 5 mixed.
	Rule        *gachaPoolRule `json:"rule,omitempty"`
}

type customGachaDocument struct {
	Pools  []customGachaPool `json:"pools"`
	NextID int               `json:"next_id,omitempty"`
}

// customGachaBase derives fixed rules from the recorded template. Legacy copies
// omit promotions; version 1 deliberately keeps the complete selected rules.
func customGachaBase(template gamestate.GachaProfile, pool customGachaPool) gamestate.GachaProfile {
	base := gamestate.CloneGachas([]gamestate.GachaProfile{template})[0]
	base.GachaID, base.GroupID = pool.GachaID, pool.GroupID
	if pool.ArthurType != 0 {
		base.ArthurType = pool.ArthurType
	}
	if pool.Name != "" {
		base.Name = pool.Name
	}
	base.PublicationKey = "operator"
	if pool.RuleVersion == 0 {
		base.DailyFirstFree, base.DailyFirstFreeSourceState = false, ""
		base.Gifts = nil
	}
	base.PlayCount = 0
	base.PoolSourceState, base.WeightSourceState = "LOCAL_POLICY", "LOCAL_POLICY"
	return base
}

func customGachaTemplateAllowed(template gamestate.GachaProfile) bool {
	return len(template.RewardPool) == 0 && len(template.Steps) == 0 && template.GroupID > 0
}

// loadCustomGachas registers stored operator pools before configurations are built. Called once at start.
func (operations *Operations) loadCustomGachas() error {
	doc, err := operations.storage.ReadDocument(customGachaKey)
	if err != nil {
		return err
	}
	operations.customGachaRevision = doc.Revision
	operations.customGachas = map[int]customGachaPool{}
	if doc.Revision == 0 {
		return nil
	}
	var stored customGachaDocument
	if err := json.Unmarshal(doc.Payload, &stored); err != nil {
		return fmt.Errorf("decode operator gacha pools: %w", err)
	}
	if stored.NextID < 0 {
		return errors.New("invalid operator gacha ID cursor")
	}
	operations.customGachaNextID = stored.NextID
	for _, pool := range stored.Pools {
		if err := operations.registerCustomGacha(pool); err != nil {
			return err
		}
	}
	return nil
}

func (operations *Operations) registerCustomGacha(pool customGachaPool) error {
	var base gamestate.GachaProfile
	if pool.Rule != nil {
		if err := pool.Rule.validate(); err != nil {
			return fmt.Errorf("operator gacha %d: %w", pool.GachaID, err)
		}
		base = pool.Rule.profile()
		base.GachaID, base.GroupID, base.Name, base.ArthurType = pool.GachaID, pool.GroupID, pool.Name, pool.ArthurType
		base.PublicationKey, base.PoolSourceState, base.WeightSourceState = "operator", "LOCAL_POLICY", "LOCAL_POLICY"
	} else {
		// Existing records retain the rules they were created with. New writes
		// never create a template reference or introduce another rule version.
		template, exists := operations.gachaBases[pool.TemplateID]
		allowed := customGachaTemplateAllowed(template)
		if pool.RuleVersion == 1 {
			allowed = template.GroupID > 0 && template.PublicationKey != "operator" && template.UserSelectMax == 0 && template.CardNum > 0 && template.CardNumMax <= 100 && gamestate.ValidateGachaRules(template) == nil
		}
		if !exists || !allowed || pool.RuleVersion < 0 || pool.RuleVersion > 1 {
			return fmt.Errorf("operator gacha %d references unavailable template %d", pool.GachaID, pool.TemplateID)
		}
		base = operations.filterGachaProfession(customGachaBase(template, pool))
	}
	if pool.ArthurType < 0 || pool.ArthurType > 5 {
		return errors.New("invalid operator gacha profession")
	}
	if _, taken := operations.gachaBases[pool.GachaID]; taken {
		return fmt.Errorf("operator gacha %d collides with an existing pool", pool.GachaID)
	}
	operations.gachaBases[pool.GachaID] = base
	operations.customGachas[pool.GachaID] = pool
	operations.managedGachaGroups[pool.GroupID] = struct{}{}
	operations.managedGachaGroupByID[pool.GachaID] = pool.GroupID
	return nil
}

func (operations *Operations) customGachaDocument() customGachaDocument {
	doc := customGachaDocument{Pools: make([]customGachaPool, 0, len(operations.customGachas)), NextID: operations.nextCustomGachaID()}
	for _, pool := range operations.customGachas {
		doc.Pools = append(doc.Pools, pool)
	}
	slices.SortFunc(doc.Pools, func(a, b customGachaPool) int { return a.GachaID - b.GachaID })
	return doc
}

// nextCustomGachaID allocates one ID used for both the pool and its publication group.
func (operations *Operations) nextCustomGachaID() int {
	next := max(customGachaFirstID, operations.customGachaNextID)
	for id := range operations.gachaBases {
		if id >= next {
			next = id + 1
		}
	}
	for group := range operations.managedGachaGroups {
		if group >= next {
			next = group + 1
		}
	}
	return next
}

// effectiveGachaConfig is what players currently get: the published document, else the base rules.
func (operations *Operations) effectiveGachaConfig(id int) (AdminGachaConfig, error) {
	config := AdminGachaConfigFromProfile(operations.gachaBases[id])
	live, err := operations.storage.ReadDocument("gacha-live:" + strconv.Itoa(id))
	if err != nil || live.Revision == 0 {
		return config, err
	}
	err = json.Unmarshal(live.Payload, &config)
	return config, err
}

func (admin *API) createCustomGacha(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		SourceID    int             `json:"source_id"`
		Rules       []gachaPoolRule `json:"rules"`
		Name        string          `json:"name"`
		Expected    *int            `json:"expected_revision"`
		Professions []int8          `json:"professions"`
	}
	if err := decodeAdminJSON(r, &body); err != nil || body.Expected == nil {
		WriteAdminError(w, 400, "请选择规则类型或复制来源、填写卡池名称并携带配置版本")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || len([]rune(name)) > 60 {
		WriteAdminError(w, 400, "卡池名称须为 1–60 字")
		return
	}
	o := admin.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	if *body.Expected != o.customGachaRevision {
		WriteAdminError(w, 409, accountstore.ErrDocumentConflict.Error())
		return
	}
	profiles := []gamestate.GachaProfile{}
	contents := []AdminGachaConfig{}
	if body.SourceID == 0 {
		if err := validateCreationRules(body.Rules); err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
		for _, rule := range body.Rules {
			p := rule.profile()
			profiles = append(profiles, p)
			contents = append(contents, AdminGachaConfigFromProfile(p))
		}
	} else {
		if len(body.Rules) != 0 {
			WriteAdminError(w, 400, "规则类型和复制来源只能选择一种")
			return
		}
		source, exists := o.gachaBases[body.SourceID]
		if copied, isCustom := o.customGachas[body.SourceID]; !exists || (isCustom && copied.Deleted) {
			WriteAdminError(w, 400, "复制来源不存在或已删除")
			return
		}
		for _, id := range o.gachaGroupMembers(source.GroupID) {
			content, err := o.effectiveGachaConfig(id)
			if err != nil {
				WriteAdminError(w, 500, err.Error())
				return
			}
			profile := adminConfiguredGacha(o.gachaBases[id], content).Profile
			rule, err := copiedGachaRule(profile)
			if err != nil || o.mixedGacha(id) {
				WriteAdminError(w, 400, "只能复制已有完整卡牌内容的普通或友情点卡池；其他规则请按类型新建")
				return
			}
			content.RandomGifts = nil
			body.Rules = append(body.Rules, rule)
			profiles, contents = append(profiles, profile), append(contents, content)
		}
	}
	if len(body.Professions) > 0 {
		if !professionChoicesAllowed(profiles) || body.Rules[0].mixed() {
			WriteAdminError(w, 400, "此规则不能使用职业选择：需要普通固定抽数卡池，且每种抽数只能有一种消耗方式")
			return
		}
		slices.Sort(body.Professions)
		for i, profession := range body.Professions {
			if profession < 1 || profession > 5 || (i > 0 && profession == body.Professions[i-1]) {
				WriteAdminError(w, 400, "职业选项须为不重复的佣兵、富豪、盗贼、歌姬或混合")
				return
			}
		}
	}
	type variant struct {
		index      int
		profession int8
	}
	variants := []variant{}
	for i, profile := range profiles {
		if len(body.Professions) == 0 {
			variants = append(variants, variant{index: i, profession: profile.ArthurType})
		}
		for _, profession := range body.Professions {
			variants = append(variants, variant{index: i, profession: profession})
		}
	}
	first := o.nextCustomGachaID()
	pools := make([]customGachaPool, len(variants))
	drafts := make([]AdminGachaConfig, len(variants))
	undo := func() {
		for _, pool := range pools {
			if pool.GachaID != 0 {
				delete(o.gachaBases, pool.GachaID)
				delete(o.customGachas, pool.GachaID)
				delete(o.managedGachaGroupByID, pool.GachaID)
			}
		}
		delete(o.managedGachaGroups, first)
	}
	for i, variant := range variants {
		rule := body.Rules[variant.index]
		pool := customGachaPool{GachaID: first + i, GroupID: first, Name: name, Rule: &rule, ArthurType: variant.profession}
		if err := o.registerCustomGacha(pool); err != nil {
			undo()
			WriteAdminError(w, 400, err.Error())
			return
		}
		pools[i] = pool
		content := contents[variant.index]
		content.GachaID, content.Name = pool.GachaID, name
		filtered := o.filterGachaProfession(adminConfiguredGacha(o.gachaBases[pool.GachaID], content).Profile)
		content.CardIDs, content.Weights, content.CardFames = filtered.CardIDs, filtered.CardWeights, filtered.CardFames
		if i > 0 {
			lead := drafts[0]
			content.CoverPath, content.StartUnix, content.EndUnix = lead.CoverPath, lead.StartUnix, lead.EndUnix
			content.OddsMessage = lead.OddsMessage
			content.DailyPlayMax, content.CategoryNum = lead.DailyPlayMax, lead.CategoryNum
		}
		if body.SourceID != 0 {
			if _, err := admin.validateGachaConfig(content); err != nil {
				undo()
				WriteAdminError(w, 400, err.Error())
				return
			}
		}
		drafts[i] = content
	}
	writes := []accountstore.DocumentWrite{{Key: customGachaKey, Expected: o.customGachaRevision, Value: o.customGachaDocument(), Operation: customGachaKey}}
	for i, pool := range pools {
		writes = append(writes, accountstore.DocumentWrite{Key: "gacha-draft:" + strconv.Itoa(pool.GachaID), Value: drafts[i], Operation: "gacha-draft"})
	}
	documents, err := o.storage.WriteDocuments(writes)
	if err != nil {
		undo()
		writeContentError(w, err)
		return
	}
	o.customGachaRevision = documents[0].Revision
	o.publishRuntimeLocked()
	// New content starts as drafts: publish the whole group, then open it in
	// 扭蛋发布. Until then players cannot see it.
	ids := make([]int, len(pools))
	for i, pool := range pools {
		ids[i] = pool.GachaID
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "gacha_ids": ids, "group_id": first, "revision": documents[0].Revision})
}

func (admin *API) changeCustomGachaDeletion(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int `json:"expected_revision"`
	}
	id, err := strconv.Atoi(chi.URLParam(r, "gachaID"))
	if err != nil || decodeAdminJSON(r, &body) != nil || body.Expected == nil {
		WriteAdminError(w, 400, "请携带卡池与配置版本")
		return
	}
	deleted := chi.URLParam(r, "action") == "delete"
	o := admin.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	pool, exists := o.customGachas[id]
	if !exists {
		WriteAdminError(w, 400, "只能删除或恢复后台新建的卡池；内置卡池请在「扭蛋发布」关闭")
		return
	}
	if *body.Expected != o.customGachaRevision || pool.Deleted == deleted {
		WriteAdminError(w, 409, accountstore.ErrDocumentConflict.Error())
		return
	}
	setGroup := func(value bool) {
		for memberID, member := range o.customGachas {
			if member.GroupID == pool.GroupID {
				member.Deleted = value
				o.customGachas[memberID] = member
			}
		}
	}
	setGroup(deleted)
	nextConfigs, _, err := o.readGachaConfigurations()
	if err != nil {
		setGroup(!deleted)
		WriteAdminError(w, 500, err.Error())
		return
	}
	doc, err := o.writeDocument(customGachaKey, o.customGachaRevision, o.customGachaDocument())
	if err != nil {
		setGroup(!deleted)
		writeContentError(w, err)
		return
	}
	o.customGachaRevision = doc.Revision
	o.gachaConfigurations = nextConfigs
	o.gachaRevision++
	o.publishRuntimeLocked()
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": doc.Revision})
}

// uploadGachaCover stores a PNG/JPEG under the covers folder named by its SHA-256, so files are immutable
// and can be mirrored to a storage container unchanged. The pool references it on its next draft.
func (admin *API) uploadGachaCover(w http.ResponseWriter, r *http.Request) {
	storeUploadedImage(w, r, admin.gachaCoverDir, gachaCoverFolder, "封面")
}

// storeUploadedImage is shared by pool covers and home banners; each kind keeps its own folder and the
// response names the file as "<folder>/<sha256>.<ext>".
func storeUploadedImage(w http.ResponseWriter, r *http.Request, dir, folder, noun string) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	if dir == "" {
		WriteAdminError(w, 503, "服务器未配置"+noun+"目录")
		return
	}
	var body struct {
		Data string `json:"data_base64"`
	}
	if err := DecodeAdminJSONLimit(r, &body, gachaCoverMaxBytes*4/3+1024); err != nil {
		WriteAdminError(w, 400, noun+"文件最大 2 MiB")
		return
	}
	data, err := base64.StdEncoding.DecodeString(body.Data)
	if err != nil || len(data) == 0 || len(data) > gachaCoverMaxBytes {
		WriteAdminError(w, 400, noun+"须为 2 MiB 以内的 PNG 或 JPEG 图片")
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		WriteAdminError(w, 400, noun+"须为 PNG 或 JPEG 图片")
		return
	}
	if config.Width < 64 || config.Height < 32 || config.Width > 4096 || config.Height > 4096 {
		WriteAdminError(w, 400, noun+"尺寸须在 64×32 到 4096×4096 之间")
		return
	}
	select {
	case gachaCoverDecodeSlot <- struct{}{}:
	case <-r.Context().Done():
		return
	}
	_, _, decodeErr := image.Decode(bytes.NewReader(data))
	<-gachaCoverDecodeSlot
	if decodeErr != nil {
		WriteAdminError(w, 400, "图片数据不完整或已损坏，请重新导出后上传")
		return
	}
	extension := map[string]string{"png": ".png", "jpeg": ".jpg"}[format]
	digest := sha256.Sum256(data)
	name := hex.EncodeToString(digest[:]) + extension
	target := filepath.Join(dir, name)
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		temp, err := os.CreateTemp(dir, ".upload-*")
		if err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
		_, writeErr := temp.Write(data)
		closeErr := temp.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(temp.Name())
			WriteAdminError(w, 500, noun+"写入失败")
			return
		}
		if err := os.Rename(temp.Name(), target); err != nil {
			_ = os.Remove(temp.Name())
			// Another upload may have committed this same content-addressed file.
			stored, readErr := os.ReadFile(target)
			if readErr != nil || !bytes.Equal(stored, data) {
				WriteAdminError(w, 500, err.Error())
				return
			}
		}
	} else if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "cover_path": folder + "/" + name, "width": config.Width, "height": config.Height, "bytes": len(data)})
}

// ServeGachaCover serves a stored cover by its content-addressed name; it is shared by the admin preview
// route and the game server's /local/gacha-covers/ route.
func ServeGachaCover(dir string) http.HandlerFunc {
	return serveUploadedImage(dir, gachaCoverFolder, gachaCoverPathPattern)
}

func serveUploadedImage(dir, folder string, pattern *regexp.Regexp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := folder + "/" + chi.URLParam(r, "file")
		if dir == "" || !pattern.MatchString(path) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", map[bool]string{true: "image/png", false: "image/jpeg"}[strings.HasSuffix(path, ".png")])
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(w, r, filepath.Join(dir, filepath.Base(path)))
	}
}

func (admin *API) validateGachaCover(path string) error {
	if path == "" {
		return nil
	}
	if !gachaCoverPathPattern.MatchString(path) {
		return errors.New("封面路径无效，请重新上传")
	}
	if _, err := os.Stat(filepath.Join(admin.gachaCoverDir, filepath.Base(path))); err != nil {
		return errors.New("封面文件不存在，请重新上传")
	}
	return nil
}

// customGachaPresets lists operator pool groups that are not deleted for the publication page, labelled
// like the built-in group they were copied from.
func (admin *API) customGachaPresets() []AdminGachaPreset {
	o := admin.operations
	result := []AdminGachaPreset{}
	for _, pool := range o.customGachaDocument().Pools {
		if pool.Deleted {
			continue
		}
		if n := len(result); n > 0 && result[n-1].GroupID == pool.GroupID {
			result[n-1].GachaIDs = append(result[n-1].GachaIDs, pool.GachaID)
			continue
		}
		preset := AdminGachaPreset{PublicationKey: "operator", GroupID: pool.GroupID, GachaIDs: []int{pool.GachaID}}
		for _, template := range admin.gachaPresets {
			if slices.Contains(template.GachaIDs, pool.TemplateID) {
				preset.BannerKey, preset.ImageURL, preset.PaymentItemID, preset.PaymentItem, preset.DrawCount = template.BannerKey, template.ImageURL, template.PaymentItemID, template.PaymentItem, template.DrawCount
			}
		}
		base := o.gachaBases[pool.GachaID]
		preset.Name, preset.Price, preset.CardCount = base.Name, base.Price, len(base.CardIDs)
		preset.CategoryNum = base.CategoryNum
		result = append(result, preset)
	}
	return result
}
