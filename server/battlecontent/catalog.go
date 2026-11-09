// Package battlecontent maps legacy and modern master-file layouts into shared
// battle definitions. It does not execute combat or own accounts and rooms.
package battlecontent

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"kairisei.local/server/battlemodel"
	"kairisei.local/server/cnformat"
)

const maxCombatMasterBytes = 16 * 1024 * 1024

// The complete original JP card table alone is about 18.8 MB. The modern
// in-memory card input also retains CN-only rows; other masters and the frozen
// file adapter keep their existing bounds.
const maxModernCardMasterBytes = 32 * 1024 * 1024

// ModernTableByteLimit is shared by the in-memory content adapter and its C
// ABI boundary. A complete card table must not be rejected by an older copy of
// the per-table bound before reaching the shared parser.
func ModernTableByteLimit(name string) int {
	if name == "card.csv" {
		return maxModernCardMasterBytes
	}
	return maxCombatMasterBytes
}

// LoadDefinitions reads source rows and validates parsing/identity boundaries.
// The engine validates capability and battle completeness before using them.
func LoadDefinitions(cardMasterPath string, battleMasterRoot string) (*battlemodel.Definitions, error) {
	cardPath, err := resolveCombatMasterFileLimit(cardMasterPath, maxModernCardMasterBytes)
	if err != nil {
		return nil, fmt.Errorf("resolve card master: %w", err)
	}
	battleRoot, err := filepath.Abs(battleMasterRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve battle master root: %w", err)
	}
	info, err := os.Stat(battleRoot)
	if err != nil || !info.IsDir() {
		return nil, errors.New("battle master root is unavailable")
	}
	gaugeText, canonicalGauges, err := readEnemyGaugeFile(battleRoot)
	if err != nil {
		return nil, err
	}
	definitions, err := loadDefinitions(func(consume func([]string) error) error {
		return readCombatCardCSV(cardPath, consume)
	}, func(name string, consume func([]string) error) error {
		root := battleRoot
		if name == "buddy.csv" {
			root = filepath.Dir(cardPath)
		}
		path, err := resolveCombatFileWithin(root, name)
		if err != nil {
			return err
		}
		if err := readCombatRows(path, name != "trance_gauge_reaction_rate.csv", consume); err != nil {
			return fmt.Errorf("load %s: %w", name, err)
		}
		return nil
	}, func(rules map[string]battlemodel.CombatRoleParameterRule) error {
		path, err := resolveCombatFileWithin(battleRoot, "skill_role_param_rule.csv")
		if err != nil {
			return err
		}
		return readCombatParameterRules(path, rules)
	}, canonicalGauges)
	if err != nil {
		return nil, err
	}
	modern := make(map[string]string)
	for _, name := range append(append([]string(nil), modernSkillTables...), TowerEnemyAITable, "card_alchemist.csv", SummonsTable, NPCTable, NPCParameterTable) {
		root := battleRoot
		if name == "card_alchemist.csv" {
			root = filepath.Dir(cardPath)
		}
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > int64(ModernTableByteLimit(name)) {
			return nil, fmt.Errorf("modern table %s is unavailable or outside the size boundary", name)
		}
		path, err = resolveCombatFileWithin(root, name)
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		modern[name] = string(raw)
	}
	if err := applyTowerEnemyAITable(definitions, modern); err != nil {
		return nil, err
	}
	if err := applyAdditionalSkillTables(definitions, modern); err != nil {
		return nil, err
	}
	if err := applyAlchemistCardTable(definitions, modern); err != nil {
		return nil, err
	}
	if err := applySummonTable(definitions, modern); err != nil {
		return nil, err
	}
	if err := applyNPCTables(definitions, modern); err != nil {
		return nil, err
	}
	err = applyEnemyGaugeTables(definitions, gaugeText, canonicalGauges)
	if err != nil {
		return nil, err
	}
	if err := bindSkillRoleMetadata(definitions); err != nil {
		return nil, err
	}
	return definitions, nil
}

// LoadDefinitionsFromTables consumes the same source text already loaded by
// the client resource layer. It shares all parsers with the file adapter and
// neither requires a server filesystem nor retains the input map or strings.
func LoadDefinitionsFromTables(tables map[string]string) (*battlemodel.Definitions, error) {
	gaugeText, canonicalGauges := tables[EnemyGaugeTable]
	if canonicalGauges && (len(gaugeText) == 0 || len(gaugeText) > maxCombatMasterBytes) {
		return nil, fmt.Errorf("invalid size for %s", EnemyGaugeTable)
	}
	read := func(name string) (io.Reader, error) {
		text, exists := tables[name]
		limit := ModernTableByteLimit(name)
		if !exists || len(text) == 0 || len(text) > limit {
			return nil, fmt.Errorf("combat table %s is missing or outside the size boundary", name)
		}
		return strings.NewReader(text), nil
	}
	definitions, err := loadDefinitions(func(consume func([]string) error) error {
		reader, err := read("card.csv")
		if err != nil {
			return err
		}
		return readCombatCardData(reader, consume)
	}, func(name string, consume func([]string) error) error {
		reader, err := read(name)
		if err != nil {
			return err
		}
		if err := readCombatData(reader, name != "trance_gauge_reaction_rate.csv", consume); err != nil {
			return fmt.Errorf("load %s: %w", name, err)
		}
		return nil
	}, func(rules map[string]battlemodel.CombatRoleParameterRule) error {
		reader, err := read("skill_role_param_rule.csv")
		if err != nil {
			return err
		}
		return readCombatParameterData(reader, rules)
	}, canonicalGauges)
	if err != nil {
		return nil, err
	}
	if err := applyTowerEnemyAITable(definitions, tables); err != nil {
		return nil, err
	}
	if err := applyAdditionalSkillTables(definitions, tables); err != nil {
		return nil, err
	}
	if err := applyAlchemistCardTable(definitions, tables); err != nil {
		return nil, err
	}
	if err := applySummonTable(definitions, tables); err != nil {
		return nil, err
	}
	if err := applyNPCTables(definitions, tables); err != nil {
		return nil, err
	}
	err = applyEnemyGaugeTables(definitions, gaugeText, canonicalGauges)
	if err != nil {
		return nil, err
	}
	if err := bindSkillRoleMetadata(definitions); err != nil {
		return nil, err
	}
	return definitions, nil
}

// The original ParseRole copies these fields from the parameter-rule table,
// after reading the per-skill role columns. Bind once after all table groups,
// including alchemist/tower, have joined the same executable definitions.
func bindSkillRoleMetadata(definitions *battlemodel.Definitions) error {
	for _, roles := range definitions.SkillRoles {
		for i := range roles {
			value, err := BindSkillRole(roles[i], definitions.RoleParamRules)
			if err != nil {
				return err
			}
			roles[i] = value
		}
	}
	return nil
}

func loadDefinitions(readCards func(func([]string) error) error, load func(string, func([]string) error) error, readRules func(map[string]battlemodel.CombatRoleParameterRule) error, canonicalGauges bool) (*battlemodel.Definitions, error) {
	catalog := &battlemodel.Definitions{
		Cards:      make(map[int]battlemodel.CombatCardDefinition),
		Spheres:    make(map[int]battlemodel.CombatSphereDefinition),
		Buddies:    make(map[int]battlemodel.CombatBuddyDefinition),
		Skills:     make(map[int][]battlemodel.CombatSkillDefinition),
		SkillRoles: make(map[int][]battlemodel.CombatSkillRole),

		RoleParamRules: make(map[string]battlemodel.CombatRoleParameterRule),
		EnemyParties:   make(map[int]battlemodel.CombatEnemyParty),
		Enemies:        make(map[int]battlemodel.CombatEnemyDefinition),
		EnemyLevels:    make(map[int]battlemodel.CombatEnemyLevel),
		EnemyAIOrders:  make(map[int]battlemodel.CombatEnemyAIOrder),
	}
	if err := readCards(func(row []string) error {
		definition, err := ParseCard(row)
		if err != nil {
			return err
		}
		if _, exists := catalog.Cards[definition.ID]; exists {
			return fmt.Errorf("duplicate card ID %d", definition.ID)
		}
		catalog.Cards[definition.ID] = definition
		return nil
	}); err != nil {
		return nil, fmt.Errorf("load card master: %w", err)
	}

	if err := load("buddy.csv", func(row []string) error {
		value, err := ParseBuddy(row)
		if err != nil {
			return err
		}
		if _, exists := catalog.Buddies[value.ID]; exists {
			return fmt.Errorf("duplicate buddy ID %d", value.ID)
		}
		catalog.Buddies[value.ID] = value
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("sphr.csv", func(row []string) error {
		value, err := ParseSphere(row)
		if err != nil {
			return err
		}
		if _, exists := catalog.Spheres[value.ID]; exists {
			return fmt.Errorf("duplicate sphere ID %d", value.ID)
		}
		catalog.Spheres[value.ID] = value
		return nil
	}); err != nil {
		return nil, err
	}
	if !canonicalGauges {
		if err := load("trance_gauge_reaction_rate.csv", func(row []string) error {
			kinds := map[string]int{"DAMAGE": 0, "ETC_DAMAGE": 1, "DEBUFF": 2, "COVERING": 3}
			kind, ok := kinds[row[0]]
			if !ok || len(row) < 11 {
				return errors.New("invalid trance reaction row")
			}
			for state := range 2 {
				for cost := range 5 {
					catalog.TranceRates[kind][state][cost] = OptionalInt(row, 1+state*5+cost)
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	if err := load("burst_gauge.csv", func(row []string) error {
		if catalog.BurstGauge.Maximum != 0 {
			return errors.New("duplicate burst gauge row")
		}
		if len(row) < 4 {
			return errors.New("burst gauge row is too short")
		}
		catalog.BurstGauge = battlemodel.CombatBurstGaugeConfig{
			NormalThreshold: OptionalInt(row, 0), Maximum: OptionalInt(row, 1),
			BreakTurns: OptionalInt(row, 2), DamageReduction: OptionalInt(row, 3),
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("burst_gauge_reaction_rate.csv", func(row []string) error {
		if len(row) < len(catalog.BurstGauge.ReactionRates) {
			return errors.New("burst gauge reaction-rate row is too short")
		}
		for index := range catalog.BurstGauge.ReactionRates {
			catalog.BurstGauge.ReactionRates[index] = OptionalInt(row, index)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("skill_player.csv", func(row []string) error {
		value, err := ParseSkill(row)
		if err == nil {
			AppendSkillVariant(catalog.Skills, value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := load("skill_role_player.csv", func(row []string) error {
		value, err := ParseSkillRole(row)
		if err == nil {
			value.RoleIndex = len(catalog.SkillRoles[value.SkillID])
			catalog.SkillRoles[value.SkillID] = append(catalog.SkillRoles[value.SkillID], value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := load("burst_skill.csv", func(row []string) error {
		value, err := ParseSkill(row)
		if err == nil {
			AppendSkillVariant(catalog.Skills, value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := load("burst_skill_role.csv", func(row []string) error {
		value, err := ParseSkillRole(row)
		if err == nil {
			value.RoleIndex = len(catalog.SkillRoles[value.SkillID])
			catalog.SkillRoles[value.SkillID] = append(catalog.SkillRoles[value.SkillID], value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := load("support_skill.csv", func(row []string) error {
		value, err := ParseSkill(row)
		if err == nil {
			AppendSkillVariant(catalog.Skills, value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := load("support_skill_lvup.csv", func(row []string) error {
		level := OptionalInt(row, 0)
		if level < 1 || level > len(catalog.SupportLoveRates) {
			return fmt.Errorf("invalid support skill level %d", level)
		}
		catalog.SupportLoveRates[level-1] = OptionalInt(row, 1)
		return nil
	}); err != nil {
		return nil, err
	}
	if rates := catalog.SupportLoveRates; rates[0] <= 0 || rates[1] <= rates[0] || rates[2] <= rates[1] || rates[2] > 100 {
		return nil, fmt.Errorf("invalid support love thresholds %v", rates)
	}
	if err := load("support_skill_role.csv", func(row []string) error {
		value, err := ParseSkillRole(row)
		if err == nil {
			value.RoleIndex = len(catalog.SkillRoles[value.SkillID])
			catalog.SkillRoles[value.SkillID] = append(catalog.SkillRoles[value.SkillID], value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := readRules(catalog.RoleParamRules); err != nil {
		return nil, fmt.Errorf("load skill_role_param_rule.csv: %w", err)
	}
	if err := load("enemy_party.csv", func(row []string) error {
		value, err := ParseEnemyParty(row)
		if err != nil {
			return err
		}
		if _, exists := catalog.EnemyParties[value.ID]; exists {
			return fmt.Errorf("duplicate enemy party ID %d", value.ID)
		}
		catalog.EnemyParties[value.ID] = value
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("enemy.csv", func(row []string) error {
		if canonicalGauges && len(row) != 32 && (len(row) != 33 || Field(row, 32) != "") {
			return errors.New("canonical enemy tables require the modern enemy columns")
		}
		value, err := ParseEnemy(row)
		if err != nil {
			return err
		}
		if _, exists := catalog.Enemies[value.ID]; exists {
			return fmt.Errorf("duplicate enemy ID %d", value.ID)
		}
		catalog.Enemies[value.ID] = value
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("enemy_lvup.csv", func(row []string) error {
		value, err := parseEnemyLevel(row, canonicalGauges)
		if err != nil {
			return err
		}
		if _, exists := catalog.EnemyLevels[value.ID]; exists {
			return fmt.Errorf("duplicate enemy level ID %d", value.ID)
		}
		catalog.EnemyLevels[value.ID] = value
		return nil
	}); err != nil {
		return nil, err
	}
	if err := load("enemy_ai_order.csv", func(row []string) error {
		return registerEnemyAIOrder(catalog, row)
	}); err != nil {
		return nil, err
	}
	if err := load("skill_enemy.csv", func(row []string) error {
		value, err := ParseSkill(row)
		if err == nil {
			AppendSkillVariant(catalog.Skills, value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if err := load("skill_role_enemy.csv", func(row []string) error {
		value, err := ParseSkillRole(row)
		if err == nil {
			value.RoleIndex = len(catalog.SkillRoles[value.SkillID])
			catalog.SkillRoles[value.SkillID] = append(catalog.SkillRoles[value.SkillID], value)
		}
		return err
	}); err != nil {
		return nil, err
	}
	return catalog, nil
}

func resolveCombatMasterFile(path string) (string, error) {
	return resolveCombatMasterFileLimit(path, maxCombatMasterBytes)
}

func resolveCombatMasterFileLimit(path string, limit int64) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return "", errors.New("combat master file is unavailable or outside the size boundary")
	}
	return absolute, nil
}

func resolveCombatFileWithin(root string, name string) (string, error) {
	path, err := resolveCombatMasterFile(filepath.Join(root, name))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("combat master file escapes its root")
	}
	return path, nil
}

// Cards use CSV.parserFast in the original client. Localized dialogue must
// not carry an RFC CSV quote state into the next card's physical line.
func readCombatCardCSV(path string, consume func([]string) error) error {
	stream, err := os.Open(path)
	if err != nil {
		return err
	}
	defer stream.Close()
	return readCombatCardData(stream, consume)
}

func readCombatCardData(stream io.Reader, consume func([]string) error) error {
	limited := &io.LimitedReader{R: stream, N: maxModernCardMasterBytes + 1}
	lines := bufio.NewScanner(limited)
	lines.Buffer(make([]byte, 4096), maxModernCardMasterBytes)
	rows := 0
	for lines.Scan() {
		row := cnformat.SplitCSVLine(lines.Text())
		row[0] = strings.TrimPrefix(row[0], "\ufeff")
		if !isDecimalCombatID(row[0]) {
			continue
		}
		if err := consume(row); err != nil {
			return fmt.Errorf("numeric card row %d: %w", rows+1, err)
		}
		rows++
	}
	if err := lines.Err(); err != nil {
		return err
	}
	if limited.N == 0 {
		return errors.New("combat card master exceeds size boundary")
	}
	if rows == 0 {
		return errors.New("combat card master contains no numeric rows")
	}
	return nil
}

func ReadCSV(path string, consume func([]string) error) error {
	return readCombatRows(path, true, consume)
}

func readCombatRows(path string, numeric bool, consume func([]string) error) error {
	stream, err := os.Open(path)
	if err != nil {
		return err
	}
	defer stream.Close()
	return readCombatData(stream, numeric, consume)
}

func readCombatData(stream io.Reader, numeric bool, consume func([]string) error) error {
	reader := csv.NewReader(io.LimitReader(stream, maxCombatMasterBytes+1))
	reader.FieldsPerRecord = -1
	// The official CN tables contain a small number of literal quote characters
	// inside otherwise unquoted display-text fields. Python's exporter preserves
	// those bytes, so accept that source dialect here instead of rewriting the
	// immutable master data.
	reader.LazyQuotes = true
	rows := 0
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if len(row) == 0 {
			continue
		}
		row[0] = strings.TrimPrefix(row[0], "\ufeff")
		if strings.HasPrefix(row[0], "#ENEMY_GAUGES_") {
			return errors.New("enemy gauge JSON extensions are retired; use canonical enemy tables")
		}
		if row[0] == "" || strings.HasPrefix(row[0], "#") || numeric && !isDecimalCombatID(row[0]) {
			continue
		}
		if err := consume(row); err != nil {
			return fmt.Errorf("data row %d: %w", rows+1, err)
		}
		rows++
	}
	if rows == 0 {
		return errors.New("combat master contains no data rows")
	}
	return nil
}

func readCombatParameterRules(path string, rules map[string]battlemodel.CombatRoleParameterRule) error {
	stream, err := os.Open(path)
	if err != nil {
		return err
	}
	defer stream.Close()
	return readCombatParameterData(stream, rules)
}

func readCombatParameterData(stream io.Reader, rules map[string]battlemodel.CombatRoleParameterRule) error {
	reader := csv.NewReader(io.LimitReader(stream, maxCombatMasterBytes+1))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if len(row) == 0 {
			continue
		}
		function := strings.TrimSpace(strings.TrimPrefix(row[0], "\ufeff"))
		if function == "" || strings.HasPrefix(function, "#") {
			continue
		}
		if _, duplicate := rules[function]; duplicate {
			return fmt.Errorf("duplicate skill role parameter rule %s", function)
		}
		rule := battlemodel.CombatRoleParameterRule{Function: function,
			PVPRate: OptionalInt(row, 1), ValueBoostUse: OptionalInt(row, 2)}
		for index := range rule.Parameters {
			rule.Parameters[index] = Field(row, 3+index)
		}
		rules[function] = rule
	}
	if len(rules) == 0 {
		return errors.New("combat parameter rules contain no functions")
	}
	return nil
}

func ParseCard(row []string) (battlemodel.CombatCardDefinition, error) {
	if len(row) >= 113 {
		return parseModernCard(row)
	}
	return parseLegacyCard(row)
}

// parseLegacyCard remains at the published CN content boundary. Modern client
// tables use the original JP prefix and never pass through this column layout.
func parseLegacyCard(row []string) (battlemodel.CombatCardDefinition, error) {
	if len(row) < 28 {
		return battlemodel.CombatCardDefinition{}, errors.New("card row is too short")
	}
	id, err := RequiredInt(row, 0, "card ID")
	if err != nil {
		return battlemodel.CombatCardDefinition{}, err
	}
	card := battlemodel.CombatCardDefinition{
		ID: id, BaseCardID: OptionalInt(row, 1), Rarity: Field(row, 7),
		MaxLevel: OptionalInt(row, 23), NormalSkillID: OptionalInt(row, 26),
		ArthurSkillID: OptionalInt(row, 27), CallSkillID: OptionalInt(row, 32), PassiveSkillID: OptionalInt(row, 33),
		LoveMax:         OptionalInt(row, 24),
		SupportSkillIDs: [4]int{OptionalInt(row, 28), OptionalInt(row, 29), OptionalInt(row, 30), OptionalInt(row, 31)},
		HPBase:          OptionalInt(row, 10),
		HPGrowth:        OptionalInt(row, 11), AttackBase: OptionalInt(row, 13),
		AttackGrowth: OptionalInt(row, 14), MagicBase: OptionalInt(row, 16),
		MagicGrowth: OptionalInt(row, 17), RecoveryBase: OptionalInt(row, 19),
		RecoveryGrowth: OptionalInt(row, 20),
	}
	for index := range card.ProfileTags {
		if tag := OptionalInt(row, 54+index); tag != -1 {
			card.ProfileTags[index] = tag
		}
	}
	return card, nil
}

func ParseSphere(row []string) (battlemodel.CombatSphereDefinition, error) {
	if len(row) < 20 {
		return battlemodel.CombatSphereDefinition{}, errors.New("sphere row is too short")
	}
	id, err := RequiredInt(row, 0, "sphere ID")
	if err != nil {
		return battlemodel.CombatSphereDefinition{}, err
	}
	return battlemodel.CombatSphereDefinition{
		ID: id, SameSphereID: OptionalInt(row, 1), Name: Field(row, 2),
		Type: strings.ToUpper(Field(row, 4)), MaxLevel: max(1, OptionalInt(row, 7)),
		SkillID: OptionalInt(row, 13), PassiveSkillID: OptionalInt(row, 14),
		CallSkillID: OptionalInt(row, 15), Count: OptionalInt(row, 17),
		PlayCondition: strings.ToUpper(Field(row, 18)), PlayParam: OptionalInt(row, 19),
	}, nil
}

func ParseBuddy(row []string) (battlemodel.CombatBuddyDefinition, error) {
	if len(row) < 25 {
		return battlemodel.CombatBuddyDefinition{}, errors.New("buddy row is too short")
	}
	id, err := RequiredInt(row, 0, "buddy ID")
	if err != nil {
		return battlemodel.CombatBuddyDefinition{}, err
	}
	value := battlemodel.CombatBuddyDefinition{
		ID: id, SameBuddyID: OptionalInt(row, 1), Name: Field(row, 2),
		Rarity: Field(row, 3), MaxLevel: OptionalInt(row, 5),
		PassiveSkillID: OptionalInt(row, 6),
	}
	for index := range value.Skills {
		offset := 7 + index*6
		value.Skills[index] = battlemodel.CombatBuddySkill{
			SkillID: OptionalInt(row, offset), CallSkillID: OptionalInt(row, offset+1),
			RequiredLevel: OptionalInt(row, offset+2), Genre: Field(row, offset+3),
			GaugeCost: OptionalInt(row, offset+4), RechargeTurn: OptionalInt(row, offset+5),
		}
	}
	return value, nil
}

func ParseSkill(row []string) (battlemodel.CombatSkillDefinition, error) {
	if len(row) < 50 {
		return battlemodel.CombatSkillDefinition{}, errors.New("skill row is too short")
	}
	id, err := RequiredInt(row, 0, "skill ID")
	if err != nil {
		return battlemodel.CombatSkillDefinition{}, err
	}
	value := battlemodel.CombatSkillDefinition{
		ID: id, Name: Field(row, 1), DisplayRole: OptionalInt(row, 5), Type: Field(row, 9), Kind: Field(row, 10),
		Attribute: Field(row, 11), Job: Field(row, 12), ArthurJob: Field(row, 12),
		DamageKind: Field(row, 13), Cost: OptionalInt(row, 14),
		PriorityPVE: OptionalInt(row, 15), PriorityPVP: OptionalInt(row, 16), Rank: Field(row, 17),
		HateRatio: OptionalInt(row, 18), Target: Field(row, 19),
		HandSelectCount: OptionalInt(row, 20), HandAttribute: Field(row, 21),
		HandKind: Field(row, 22), HandMinCost: OptionalInt(row, 23), HandMaxCost: OptionalInt(row, 24),
		AppendTrigger: Field(row, 28), AppendDuration: OptionalInt(row, 29), AppendCondition: Field(row, 30),
		BranchCondition: Field(row, 36), BranchCondition2: Field(row, 42), BranchPriority: OptionalInt(row, 48),
		FunctionID: OptionalInt(row, 49),
	}
	for index := range value.Groups {
		value.Groups[index] = OptionalInt(row, 25+index)
	}
	for index := range value.BranchParameters {
		value.AppendParameters[index] = Field(row, 31+index)
		value.BranchParameters[index] = Field(row, 37+index)
		value.BranchParameters2[index] = Field(row, 43+index)
	}
	if value.FunctionID == 0 {
		value.FunctionID = value.ID
	}
	return value, nil
}

func ParseSkillRole(row []string) (battlemodel.CombatSkillRole, error) {
	if len(row) < 32 {
		return battlemodel.CombatSkillRole{}, errors.New("skill role row is too short")
	}
	id, err := RequiredInt(row, 0, "skill role ID")
	if err != nil {
		return battlemodel.CombatSkillRole{}, err
	}
	hateLimit := 1000000
	if parsed, err := strconv.ParseInt(strings.TrimSpace(row[31]), 10, 32); err == nil {
		hateLimit = int(parsed)
	}
	value := battlemodel.CombatSkillRole{
		SkillID: id, Effect2D: Field(row, 1), Effect3D: Field(row, 3),
		HitEffect: Field(row, 4), HitPosition: Field(row, 5),
		Function: Field(row, 8), Target: Field(row, 9),
		ExcludeSelf: OptionalInt(row, 10) == 1, HasTargetAttributes: true,
		ChainRate: OptionalInt(row, 30), HateLimit: hateLimit,
	}
	for index := range value.Attributes {
		value.Attributes[index] = OptionalInt(row, 11+index) > 0
	}
	for index := range value.Parameters {
		value.Parameters[index] = strings.TrimSpace(row[20+index])
	}
	return value, nil
}

func ParseEnemyParty(row []string) (battlemodel.CombatEnemyParty, error) {
	if len(row) < 16 {
		return battlemodel.CombatEnemyParty{}, errors.New("enemy party row is too short")
	}
	id, err := RequiredInt(row, 0, "enemy party ID")
	if err != nil {
		return battlemodel.CombatEnemyParty{}, err
	}
	value := battlemodel.CombatEnemyParty{ID: id, Placement: Field(row, 1), BGMID: OptionalInt(row, 14), FieldID: OptionalInt(row, 15), HintID: OptionalInt(row, 16)}
	for index := range value.Slots {
		base := 2 + index*3
		value.Slots[index] = battlemodel.CombatEnemyPartySlot{EnemyID: OptionalInt(row, base), HPRate: OptionalInt(row, base+1), ParentIndex: OptionalInt(row, base+2)}
		if value.Slots[index].HPRate == 0 {
			value.Slots[index].HPRate = 1
		}
	}
	return value, nil
}

func ParseEnemy(row []string) (battlemodel.CombatEnemyDefinition, error) {
	if len(row) == 32 || len(row) == 33 {
		return parseModernEnemy(row)
	}
	return parseLegacyEnemy(row)
}

func parseLegacyEnemy(row []string) (battlemodel.CombatEnemyDefinition, error) {
	if len(row) < 25 {
		return battlemodel.CombatEnemyDefinition{}, errors.New("enemy row is too short")
	}
	id, err := RequiredInt(row, 0, "enemy ID")
	if err != nil {
		return battlemodel.CombatEnemyDefinition{}, err
	}
	tribes := []int{OptionalInt(row, 1)}
	// The modern common master retains the original 28-column prefix so the
	// original presentation parser still reads model/voice fields unchanged.
	// Its explicit extension restores the two memberships omitted by the CN
	// wire layout. An unmarked row continues to have exactly one tribe slot.
	if len(row) > 28 && Field(row, 28) != "" {
		if Field(row, 28) != "TRIBES_V1" || len(row) != 31 {
			return battlemodel.CombatEnemyDefinition{}, fmt.Errorf("enemy %d has an unknown tribe extension", id)
		}
		for _, column := range []int{29, 30} {
			value, err := strconv.ParseInt(Field(row, column), 10, 32)
			if err != nil || value < 0 {
				return battlemodel.CombatEnemyDefinition{}, fmt.Errorf("enemy %d has an invalid tribe slot", id)
			}
			tribes = append(tribes, int(value))
		}
	}
	return battlemodel.CombatEnemyDefinition{
		ID: id, TribeIDs: tribes, ModelID: Field(row, 2),
		ImageID: OptionalInt(row, 4), Live2DName: Field(row, 5), Name: Field(row, 6),
		Attribute: Field(row, 7), HP: OptionalInt(row, 8), Attack: OptionalInt(row, 9),
		Magic: OptionalInt(row, 10), Recovery: OptionalInt(row, 11),
		Defense: OptionalInt(row, 12), MagicDefense: OptionalInt(row, 13),
		DamageReduction: OptionalInt(row, 14), Size: Field(row, 24),
		AttributeFixed: [5]int{OptionalInt(row, 15), OptionalInt(row, 16), OptionalInt(row, 17), OptionalInt(row, 18), OptionalInt(row, 19)},
		TranceLimit:    OptionalInt(row, 20), OverheatLimit: OptionalInt(row, 21),
		OverheatTurns: OptionalInt(row, 22), OverheatResist: OptionalInt(row, 23),
	}, nil
}

func ParseEnemyLevel(row []string) (battlemodel.CombatEnemyLevel, error) {
	return parseEnemyLevel(row, len(row) >= 334)
}

func parseEnemyLevel(row []string, canonical bool) (battlemodel.CombatEnemyLevel, error) {
	if (!canonical && len(row) < 313) || (canonical && (len(row) < 30 || len(row) > 335)) {
		return battlemodel.CombatEnemyLevel{}, errors.New("enemy level row has an unsupported number of columns")
	}
	id, err := RequiredInt(row, 0, "enemy level ID")
	if err != nil {
		return battlemodel.CombatEnemyLevel{}, err
	}
	value := battlemodel.CombatEnemyLevel{
		ID: id, HPBars: OptionalInt(row, 1), InitialAwake: OptionalInt(row, 27),
		ActionsPerTurn: OptionalInt(row, 28), PassiveSkillID: OptionalInt(row, 29),
	}
	for index := range value.AttributeRates {
		value.AttributeRates[index] = OptionalInt(row, 2+index)
	}
	for index := 1; index <= 11; index++ {
		value.StatusResistances[index] = OptionalInt(row, 10+index)
	}
	for index := range value.DOTReductions {
		value.DOTReductions[index] = OptionalInt(row, 22+index)
	}
	callStart := 30
	if canonical {
		for state := range value.GaugeActions {
			offset := 30 + state*7
			skillID := OptionalInt(row, offset)
			if skillID <= 0 {
				continue
			}
			action := battlemodel.CombatEnemyAction{Slot: state, Category: "gauge", SkillID: skillID, Target: Field(row, offset+1)}
			for parameter := range action.TargetParams {
				action.TargetParams[parameter] = Field(row, offset+2+parameter)
			}
			value.GaugeActions[state] = action
		}
		callStart += 21
	}
	for index := range value.CallSkillIDs {
		value.CallSkillIDs[index] = OptionalInt(row, callStart+index)
	}
	actionSlot := 0
	appendAction := func(category string, offset int) {
		skillID := OptionalInt(row, offset)
		if skillID == 0 {
			actionSlot++
			return
		}
		action := battlemodel.CombatEnemyAction{
			Slot: actionSlot, Category: category, SkillID: skillID, AIConditionID: OptionalInt(row, offset+1),
			Priority: OptionalInt(row, offset+2), Target: Field(row, offset+3),
			ActionCost: OptionalInt(row, offset+9), MaxUses: OptionalInt(row, offset+10),
			Rate: OptionalInt(row, offset+11), CountOnMiss: OptionalInt(row, offset+12) != 0,
		}
		for index := range action.TargetParams {
			action.TargetParams[index] = Field(row, offset+4+index)
		}
		value.Actions = append(value.Actions, action)
		actionSlot++
	}
	shift := callStart - 30
	appendAction("normal", 40+shift)
	for offset := 53 + shift; offset <= 222+shift; offset += 13 {
		appendAction("skill", offset)
	}
	for offset := 235 + shift; offset <= 287+shift; offset += 13 {
		appendAction("special", offset)
	}
	appendAction("death", 300+shift)
	return value, nil
}

func Field(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	value := strings.TrimSpace(row[index])
	if strings.EqualFold(value, "NULL") {
		return ""
	}
	return value
}

func OptionalInt(row []string, index int) int {
	value := Field(row, index)
	if value == "" {
		return 0
	}
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func RequiredInt(row []string, index int, label string) (int, error) {
	value := Field(row, index)
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s is invalid", label)
	}
	return parsed, nil
}

func isDecimalCombatID(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
