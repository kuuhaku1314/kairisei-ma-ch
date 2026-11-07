package battlecore

import (
	"encoding/json"
	"kairisei.local/server/battlecontent"
	"os"
	"strings"
	"testing"
)

type nativeActionAPISkill struct {
	ID               int
	SkillType        string `json:"skill_type"`
	Kind             string
	Attribute        string
	Target           string
	Priority         int
	Cost             *int
	AppendTrigger    string    `json:"append_trigger"`
	AppendDuration   int       `json:"append_duration"`
	AppendCondition  string    `json:"append_condition"`
	AppendParameters [5]string `json:"append_parameters"`
	CSVRow           []string  `json:"csv_row"`
	Variants         []nativeActionAPISkill
	OriginalSlot     *int     `json:"original_slot"`
	OrderRow         []string `json:"order_row"`
	Roles            []struct {
		Function   string
		Parameters [10]string
		Target     string
		Chain      int
		CSVRow     []string `json:"csv_row"`
	}
}

type nativeActionAPIEnemy struct {
	Member          int
	HPRate          int `json:"hp_rate"`
	Parent          int
	EnemyRow        []string               `json:"enemy_row"`
	LevelRow        []string               `json:"level_row"`
	ActionsPerTurn  *int                   `json:"actions_per_turn"`
	InitialAwake    *int                   `json:"initial_awake"`
	OfficialActions bool                   `json:"official_actions"`
	EnemySkill      []nativeActionAPISkill `json:"enemy_skills"`
	PassiveSkill    *nativeActionAPISkill  `json:"passive_skill"`
	DeathSkill      *nativeActionAPISkill  `json:"death_skill"`
	CallSkills      []struct {
		Slot  int
		Skill nativeActionAPISkill
	} `json:"call_skills"`
	Drops []struct {
		Type         int
		Num          int
		RewardTypeID int `json:"reward_typeid"`
	}
}

type nativeActionAPICard struct {
	CardID  int      `json:"card_id"`
	CSVRow  []string `json:"csv_row"`
	Skill   nativeActionAPISkill
	Bonus   nativeActionAPISkill
	Call    nativeActionAPISkill
	Passive nativeActionAPISkill
}

func TestNativeTacticalAutomatonAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_TACTICAL_AUTOMATON_API_RECEIPT"), "original-cn-x86-tactical-automaton-api-chain")
}

func TestNativeScoreLimitAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_SCORE_LIMIT_API_RECEIPT"), "original-cn-x86-score-limit-api-chain")
}

// Keep the official slot identities and source rows, but register only the
// controlled skill_set calls actually supplied to the original API.
func nativeActionAPIParty(t *testing.T, engine *BattleEngine, row []string, enemies []nativeActionAPIEnemy) (int, []BattleDrop) {
	t.Helper()
	party, err := battlecontent.ParseEnemyParty(row)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, slot := range party.Slots {
		if slot.EnemyID != 0 {
			count++
		}
	}
	if len(enemies) != count || count == 0 {
		t.Fatal("receipt must contain every nonzero official party slot")
	}
	seen := [4]bool{}
	var drops []BattleDrop
	if engine.catalog.Skills == nil {
		engine.catalog.Skills = map[int][]CombatSkillDefinition{}
		engine.catalog.SkillRoles = map[int][]CombatSkillRole{}
	}
	for _, input := range enemies {
		i := input.Member - 5
		if i < 0 || i >= len(seen) || seen[i] {
			t.Fatal("invalid or duplicate original enemy member", input.Member)
		}
		seen[i] = true
		definition, definitionErr := battlecontent.ParseEnemy(input.EnemyRow)
		level, levelErr := battlecontent.ParseEnemyLevel(input.LevelRow)
		if definitionErr != nil || levelErr != nil || party.Slots[i].EnemyID != definition.ID ||
			level.ID != definition.ID || party.Slots[i].ParentIndex != input.Parent ||
			battlecontent.OptionalInt(row, 3+3*i) != input.HPRate {
			t.Fatalf("invalid original enemy %d: definition=%v level=%v", i, definitionErr, levelErr)
		}
		officialLevel := level
		level.PassiveSkillID, level.CallSkillIDs, level.Actions = 0, [10]int{}, nil
		register := func(input nativeActionAPISkill) {
			variants := input.Variants
			if len(variants) == 0 {
				variants = []nativeActionAPISkill{input}
			}
			engine.catalog.Skills[input.ID] = nil
			for _, variant := range variants {
				s, roles := variant.definitions()
				if s.ID != input.ID {
					t.Fatal("variant belongs to a different skill")
				}
				engine.catalog.Skills[s.ID] = append(engine.catalog.Skills[s.ID], s)
				engine.catalog.SkillRoles[s.FunctionID] = roles
			}
		}
		if input.PassiveSkill != nil {
			if input.PassiveSkill.ID != officialLevel.PassiveSkillID {
				t.Fatal("passive is not the official level reference")
			}
			register(*input.PassiveSkill)
			level.PassiveSkillID = input.PassiveSkill.ID
		}
		for _, call := range input.CallSkills {
			if call.Slot < 0 || call.Slot >= 10 || officialLevel.CallSkillIDs[call.Slot] != call.Skill.ID {
				t.Fatal("CALL is not the official level reference")
			}
			register(call.Skill)
			level.CallSkillIDs[call.Slot] = call.Skill.ID
		}
		if input.DeathSkill != nil {
			found := false
			for _, action := range officialLevel.Actions {
				found = found || action.Category == "death" && action.SkillID == input.DeathSkill.ID
			}
			if !found {
				t.Fatal("death skill is not the official level reference")
			}
			register(*input.DeathSkill)
			level.Actions = append(level.Actions, CombatEnemyAction{Slot: 20, Category: "death", SkillID: input.DeathSkill.ID,
				Target: "RANDOM", ActionCost: 1, MaxUses: 99, Rate: 100, Priority: input.DeathSkill.Priority})
		}
		if input.ActionsPerTurn != nil {
			level.ActionsPerTurn = *input.ActionsPerTurn
		}
		if input.InitialAwake != nil {
			level.InitialAwake = *input.InitialAwake
		}
		for index, es := range input.EnemySkill {
			register(es)
			if input.OfficialActions {
				if len(input.EnemySkill) != len(officialLevel.Actions) || input.DeathSkill != nil ||
					es.OriginalSlot == nil || *es.OriginalSlot != officialLevel.Actions[index].Slot || es.ID != officialLevel.Actions[index].SkillID ||
					!ValidEnemyAIOrderFields(es.OrderRow) ||
					battlecontent.OptionalInt(es.OrderRow, 0) != officialLevel.Actions[index].AIConditionID {
					t.Fatal("official action or AI order registration is incomplete")
				}
				if engine.catalog.EnemyAIOrders == nil {
					engine.catalog.EnemyAIOrders = map[int]CombatEnemyAIOrder{}
				}
				id := officialLevel.Actions[index].AIConditionID
				// ParseTrigger returns Trigger.Empty for NULL without reading its
				// eight parameters; official rows can omit this unused tail.
				engine.catalog.EnemyAIOrders[id] = CombatEnemyAIOrder{ID: id, Fields: es.OrderRow}
				level.Actions = append(level.Actions, officialLevel.Actions[index])
				continue
			}
			level.Actions = append(level.Actions, CombatEnemyAction{Slot: index + 1, SkillID: es.ID, Category: "skill",
				Target: "RANDOM", ActionCost: 1, MaxUses: 99, Rate: 100, Priority: es.Priority})
		}
		engine.catalog.Enemies[definition.ID], engine.catalog.EnemyLevels[level.ID] = definition, level
		for _, drop := range input.Drops {
			drops = append(drops, BattleDrop{EnemyIndex: i, RewardType: drop.Type, Num: drop.Num, RewardTypeID: drop.RewardTypeID})
		}
	}
	engine.catalog.EnemyParties[party.ID] = party
	return party.ID, drops
}

func (s nativeActionAPISkill) definitions() (CombatSkillDefinition, []CombatSkillRole) {
	skill := CombatSkillDefinition{Type: s.SkillType, ID: s.ID, FunctionID: s.ID, DisplayRole: 1, Kind: s.Kind, Attribute: s.Attribute,
		Job: "NULL", Target: s.Target, Cost: 1, HateRatio: 100, PriorityPVE: s.Priority, DamageKind: "PHYSICS",
		AppendTrigger: s.AppendTrigger, AppendDuration: s.AppendDuration,
		AppendCondition: s.AppendCondition, AppendParameters: s.AppendParameters}
	if s.Cost != nil {
		skill.Cost = *s.Cost
	}
	roles := make([]CombatSkillRole, len(s.Roles))
	if len(s.CSVRow) != 0 {
		var err error
		skill, err = battlecontent.ParseSkill(s.CSVRow)
		if err != nil {
			panic(err)
		}
	}
	for i, r := range s.Roles {
		roles[i] = CombatSkillRole{SkillID: s.ID, RoleIndex: i, Function: r.Function, Target: r.Target, Parameters: r.Parameters,
			ChainRate: r.Chain, HateLimit: 100, HasTargetAttributes: true,
			Attributes: [9]bool{true, true, true, true, true, true, true, true, true}}
		if len(r.CSVRow) != 0 {
			var err error
			roles[i], err = battlecontent.ParseSkillRole(r.CSVRow)
			if err != nil {
				panic(err)
			}
			roles[i].RoleIndex = i
		}
	}
	return skill, roles
}

func registerNativeActionSkill(t *testing.T, input nativeActionAPISkill, skills map[int][]CombatSkillDefinition, roleSets map[int][]CombatSkillRole) {
	t.Helper()
	if input.ID == 0 {
		return
	}
	variants := input.Variants
	if len(variants) == 0 {
		variants = []nativeActionAPISkill{input}
	}
	skills[input.ID] = nil
	for _, variant := range variants {
		s, roles := variant.definitions()
		if s.ID != input.ID {
			t.Fatal("variant belongs to a different skill")
		}
		skills[input.ID] = append(skills[input.ID], s)
		roleSets[s.FunctionID] = roles
	}
}

// This optional integration test compares original API output verbatim, not
// hand-written expected formulas. No general debug/parameter rows are hidden.
func TestNativeActionAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_ACTION_API_RECEIPT"), "original-cn-x86-synthetic-action-api-chain")
}

func TestNativeOfficialAppendAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_OFFICIAL_APPEND_API_RECEIPT"), "original-cn-x86-official-append-skill-api-chain")
}

func TestNativeOfficialSphereAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_OFFICIAL_SPHERE_API_RECEIPT"), "original-cn-x86-official-sphere-skill-api-chain")
}

func TestNativeOfficialBuddyAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_OFFICIAL_BUDDY_API_RECEIPT"), "original-cn-x86-official-buddy-skill-api-chain")
}

func TestNativeResumeAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_RESUME_API_RECEIPT"), "original-cn-x86-resume-api-chain")
}

func TestNativeWaveAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_WAVE_API_RECEIPT"), "original-cn-x86-wave-api-chain")
}

func TestNativeLifecycleAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_LIFECYCLE_API_RECEIPT"), "original-cn-x86-lifecycle-api-chain")
}

func TestNativeOfficialPartyAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_OFFICIAL_PARTY_API_RECEIPT"), "original-cn-x86-official-full-party-attribute-api-chain")
}

func TestNativeAwakeDropAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_AWAKE_DROP_API_RECEIPT"), "original-cn-x86-official-awake-drop-next-wave-api-chain")
}

func TestNativeEnemyRegistrationAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_ENEMY_REGISTRATION_API_RECEIPT"), "original-cn-x86-official-enemy-registration-api-chain")
}

func TestNativeEnemySchedulerAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_ENEMY_SCHEDULER_API_RECEIPT"), "original-cn-x86-official-enemy-scheduler-api-chain")
}

func TestNativeAttributeEffectAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_ATTRIBUTE_EFFECT_API_RECEIPT"), "original-cn-x86-official-attribute-effect-api-chain")
}

func TestNativeOfficialEnemyCallAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_OFFICIAL_ENEMY_CALL_API_RECEIPT"), "original-cn-x86-official-enemy-call-scheduler-api-chain")
}

func TestNativePlayerDrainAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_PLAYER_DRAIN_API_RECEIPT"), "original-cn-x86-player-drain-api-chain")
}

func TestNativeAttackCallEdgesAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_ATTACK_CALL_EDGES_API_RECEIPT"), "original-cn-x86-attack-call-edges-api-chain")
}

func TestNativePartsGutsAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_PARTS_GUTS_API_RECEIPT"), "original-cn-x86-parts-guts-api-chain")
}

func TestNativeSkillDeathAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_SKILL_DEATH_API_RECEIPT"), "original-cn-x86-skill-death-api-chain")
}

func TestNativeNestedDeathAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_NESTED_DEATH_API_RECEIPT"), "original-cn-x86-nested-death-api-chain")
}

func TestNativeParameterHPAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_PARAMETER_HP_API_RECEIPT"), "original-cn-x86-parameter-hp-api-chain")
}

func TestNativeDeathCardBuffAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_DEATH_CARD_BUFF_API_RECEIPT"), "original-cn-x86-death-card-buff-api-chain")
}

func TestNativeEnemyChainAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_ENEMY_CHAIN_API_RECEIPT"), "original-cn-x86-enemy-chain-api-chain")
}

func TestNativeCostBlockAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_COST_BLOCK_API_RECEIPT"), "original-cn-x86-cost-block-api-chain")
}

func TestNativeEnemyDebuffAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_ENEMY_DEBUFF_API_RECEIPT"), "original-cn-x86-enemy-debuff-api-chain")
}

func TestNativeEmptyTargetAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_EMPTY_TARGET_API_RECEIPT"), "original-cn-x86-empty-target-api-chain")
}

func TestNativeDamageTriggerAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_DAMAGE_TRIGGER_API_RECEIPT"), "original-cn-x86-damage-trigger-api-chain")
}

func TestNativeEnemyLivenessAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_ENEMY_LIVENESS_API_RECEIPT"), "original-cn-x86-enemy-liveness-api-chain")
}

func TestNativeTriggerTargetAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_TRIGGER_TARGET_API_RECEIPT"), "original-cn-x86-trigger-target-api-chain")
}

func TestNativeCardCountAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_CARD_COUNT_API_RECEIPT"), "original-cn-x86-card-count-api-chain")
}

func TestNativeTurnStatusAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_TURN_STATUS_API_RECEIPT"), "original-cn-x86-turn-status-api-chain")
}

func TestNativePartDamageAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_PART_DAMAGE_API_RECEIPT"), "original-cn-x86-part-damage-api-chain")
}

func TestNativeBuffTriggerAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_BUFF_TRIGGER_API_RECEIPT"), "original-cn-x86-buff-trigger-api-chain")
}

func TestNativeDamageBalanceAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_DAMAGE_BALANCE_API_RECEIPT"), "original-cn-x86-damage-balance-api-chain")
}

func TestNativeDeathCountAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_DEATH_COUNT_API_RECEIPT"), "original-cn-x86-death-count-api-chain")
}

func TestNativeHealKindAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_HEAL_KIND_API_RECEIPT"), "original-cn-x86-heal-kind-api-chain")
}

func TestNativeStatusOwnerAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_STATUS_OWNER_API_RECEIPT"), "original-cn-x86-status-owner-api-chain")
}

func TestNativeBarrierStateAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_BARRIER_STATE_API_RECEIPT"), "original-cn-x86-barrier-state-api-chain")
}

func TestNativePlayerDeathAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_PLAYER_DEATH_API_RECEIPT"), "original-cn-x86-player-death-api-chain")
}

func TestNativeTotalDamageAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_TOTAL_DAMAGE_API_RECEIPT"), "original-cn-x86-total-damage-api-chain")
}

func TestNativeJobDebuffAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_JOB_DEBUFF_API_RECEIPT"), "original-cn-x86-job-debuff-api-chain")
}

func TestNativeHandCountAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_HAND_COUNT_API_RECEIPT"), "original-cn-x86-hand-count-api-chain")
}

func TestNativeCurseCostAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_CURSE_COST_API_RECEIPT"), "original-cn-x86-curse-cost-api-chain")
}

func TestNativeTargetBuffAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_TARGET_BUFF_API_RECEIPT"), "original-cn-x86-target-buff-api-chain")
}

func TestNativeHPFixedAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_HP_FIXED_API_RECEIPT"), "original-cn-x86-hp-fixed-api-chain")
}

func TestNativeTargetAttrAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_TARGET_ATTR_API_RECEIPT"), "original-cn-x86-target-attr-api-chain")
}

func TestNativeUserDebuffAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_USER_DEBUFF_API_RECEIPT"), "original-cn-x86-user-debuff-api-chain")
}

func TestNativeUserBlessAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_USER_BLESS_API_RECEIPT"), "original-cn-x86-user-bless-api-chain")
}

func TestNativeTargetHPAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_TARGET_HP_API_RECEIPT"), "original-cn-x86-target-hp-api-chain")
}

func TestNativeDeadNowAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_DEAD_NOW_API_RECEIPT"), "original-cn-x86-dead-now-api-chain")
}

func TestNativeTargetDebuffAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_TARGET_DEBUFF_API_RECEIPT"), "original-cn-x86-target-debuff-api-chain")
}

func TestNativeTargetMetricAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_TARGET_METRIC_API_RECEIPT"), "original-cn-x86-target-metric-api-chain")
}

func TestNativeEnemyHealTargetAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_ENEMY_HEAL_TARGET_API_RECEIPT"), "original-cn-x86-enemy-heal-target-api-chain")
}

func TestNativeHealStateAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_HEAL_STATE_API_RECEIPT"), "original-cn-x86-heal-state-api-chain")
}

func TestNativeTurnRevengeAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_TURN_REVENGE_API_RECEIPT"), "original-cn-x86-turn-revenge-api-chain")
}

func TestNativeHideReleaseAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_HIDE_RELEASE_API_RECEIPT"), "original-cn-x86-hide-release-api-chain")
}

func TestNativeDefenseTargetAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_DEFENSE_TARGET_API_RECEIPT"), "original-cn-x86-defense-target-api-chain")
}

func TestNativeMemberTargetAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_MEMBER_TARGET_API_RECEIPT"), "original-cn-x86-member-target-api-chain")
}

func TestNativeHateDebuffAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_HATE_DEBUFF_API_RECEIPT"), "original-cn-x86-hate-debuff-api-chain")
}

func TestNativeWeaknessTargetAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_WEAKNESS_TARGET_API_RECEIPT"), "original-cn-x86-weakness-target-api-chain")
}

func TestNativePartsClosureAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_PARTS_CLOSURE_API_RECEIPT"), "original-cn-x86-parts-closure-api-chain")
}

func TestNativeHPDrawClosureAPIReceipt(t *testing.T) {
	compareNativeActionAPIReceipt(t, os.Getenv("CN_NATIVE_HP_DRAW_CLOSURE_API_RECEIPT"), "original-cn-x86-hp-draw-closure-api-chain")
}

func compareNativeActionAPIReceipt(t *testing.T, path, scope string) {
	t.Helper()
	if path == "" {
		t.Skip("set CN_NATIVE_ACTION_API_RECEIPT to an original action API receipt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Scope   string
		Library string `json:"lib_sha256"`
		Cases   []struct {
			ScoreDamage *int64 `json:"score_damage"`
			Input       struct {
				EndTurn          int          `json:"end_turn"`
				MemberDeadEnd    bool         `json:"member_dead_end"`
				BurstGauge       []int        `json:"burst_gauge"`
				BurstInitial     int          `json:"burst_initial"`
				TranceRates      [4][2][5]int `json:"trance_rates"`
				Name             string
				Seed             *uint32
				OfficialPartyRow []string               `json:"official_party_row"`
				Enemies          []nativeActionAPIEnemy `json:"enemies"`
				EnemyHP          int                    `json:"enemy_hp"`
				EnemySkill       []nativeActionAPISkill `json:"enemy_skills"`
				Waves            []struct {
					OfficialPartyRow []string `json:"official_party_row"`
					Enemies          []nativeActionAPIEnemy
					EnemyHP          int                    `json:"enemy_hp"`
					EnemySkill       []nativeActionAPISkill `json:"enemy_skills"`
				}
				Players []struct {
					Deck     []nativeActionAPICard
					HP       int
					MaxHP    int `json:"max_hp"`
					Attack   int
					Magic    int
					Recovery int
					Skill    nativeActionAPISkill
					CardID   int `json:"card_id"`
					Bonus    nativeActionAPISkill
					Call     nativeActionAPISkill
					Spheres  []struct {
						Slot    int
						Level   int
						CSVRow  []string `json:"csv_row"`
						Skill   nativeActionAPISkill
						Call    nativeActionAPISkill
						Support nativeActionAPISkill
					}
					Buddies []struct {
						Slot    int
						Level   int
						CSVRow  []string `json:"csv_row"`
						Passive nativeActionAPISkill
						Skills  []struct{ Skill, Call nativeActionAPISkill }
					}
				}
			}
			Phases []struct {
				HaveDrops  [][3]int `json:"have_drops"`
				Name       string
				Wave       int
				CSV        string
				RNG        [4]uint32
				Submission struct {
					Member       int
					Cards        [5]int
					Targets      [5]int
					SphereSlot   int `json:"sphere_slot"`
					SphereTarget int `json:"sphere_target"`
				}
				Reservation struct {
					Member int
					Slot   int
				}
				Burst struct {
					Member, Target, Slot int
					Cards                [5]int
				}
			}
		}
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Scope != scope || receipt.Library != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Cases) == 0 {
		t.Fatal("wrong original action API receipt")
	}
	for _, specimen := range receipt.Cases {
		t.Run(specimen.Input.Name, func(t *testing.T) {
			engine, members := coreBattleFixture(t)
			engine.endTurn = specimen.Input.EndTurn
			engine.memberDeadEnd = specimen.Input.MemberDeadEnd
			engine.catalog.TranceRates = specimen.Input.TranceRates
			if specimen.Input.Seed != nil {
				engine.seed = *specimen.Input.Seed
				engine.rng = newXorShift128(engine.seed)
			}
			if g := specimen.Input.BurstGauge; len(g) != 0 {
				if len(g) != 9 {
					t.Fatal("invalid native gauge configuration")
				}
				engine.catalog.BurstGauge = CombatBurstGaugeConfig{NormalThreshold: g[0], Maximum: g[1], BreakTurns: g[2], DamageReduction: g[3]}
				copy(engine.catalog.BurstGauge.ReactionRates[:], g[4:])
			}
			if len(specimen.Input.Players) != 4 {
				t.Fatal("expected four explicit input players")
			}
			for i, p := range specimen.Input.Players {
				player := &engine.players[i]
				player.HP, player.MaxHP, player.BaseMaxHP = p.HP, p.MaxHP, p.MaxHP
				player.Attack, player.BaseAttack = p.Attack, p.Attack
				player.Magic, player.BaseMagic = p.Magic, p.Magic
				player.Recovery, player.BaseRecovery = p.Recovery, p.Recovery
				deck := p.Deck
				if len(deck) == 0 {
					cardID := p.CardID
					if cardID == 0 {
						cardID = i + 1
					}
					for range 10 {
						deck = append(deck, nativeActionAPICard{CardID: cardID, Skill: p.Skill, Bonus: p.Bonus, Call: p.Call})
					}
				}
				if len(deck) != 10 {
					t.Fatal("expected ten explicit deck cards")
				}
				for slot, card := range deck {
					definition := CombatCardDefinition{ID: card.CardID, NormalSkillID: card.Skill.ID, ArthurSkillID: card.Bonus.ID, CallSkillID: card.Call.ID, PassiveSkillID: card.Passive.ID}
					if len(card.CSVRow) != 0 {
						var err error
						definition, err = battlecontent.ParseCard(card.CSVRow)
						if err != nil || definition.ID != card.CardID || definition.NormalSkillID != card.Skill.ID || definition.ArthurSkillID != card.Bonus.ID || definition.CallSkillID != card.Call.ID || definition.PassiveSkillID != card.Passive.ID {
							t.Fatal("original deck card identity/skills mismatch", card.CardID, err)
						}
					}
					engine.catalog.Cards[card.CardID] = definition
					for _, input := range []nativeActionAPISkill{card.Skill, card.Bonus, card.Call} {
						registerNativeActionSkill(t, input, engine.catalog.Skills, engine.catalog.SkillRoles)
					}
					if card.Passive.ID != 0 {
						if engine.catalog.Skills == nil {
							engine.catalog.Skills = map[int][]CombatSkillDefinition{}
							engine.catalog.SkillRoles = map[int][]CombatSkillRole{}
						}
						registerNativeActionSkill(t, card.Passive, engine.catalog.Skills, engine.catalog.SkillRoles)
						var err error
						err = engine.catalog.ValidateMainCardPassives(definition)
						if err != nil {
							t.Fatal(err)
						}
					}
					player.Deck[slot].CardID = card.CardID
					members[i].DeckCards[slot].CardID = card.CardID
				}
				for _, sphere := range p.Spheres {
					definition, err := battlecontent.ParseSphere(sphere.CSVRow)
					if err != nil || sphere.Slot < 1 || sphere.Slot > 3 {
						t.Fatalf("invalid original sphere input: %v", err)
					}
					if engine.catalog.Spheres == nil {
						engine.catalog.Spheres = map[int]CombatSphereDefinition{}
					}
					engine.catalog.Spheres[definition.ID] = definition
					player.Spheres[sphere.Slot-1] = battleSphere{Slot: sphere.Slot, SphereID: definition.ID, Level: sphere.Level,
						Type: definition.Type, Count: definition.Count, Maximum: definition.Count}
					for _, extra := range []nativeActionAPISkill{sphere.Skill, sphere.Call} {
						registerNativeActionSkill(t, extra, engine.catalog.Skills, engine.catalog.SkillRoles)
					}
					if sphere.Support.ID != 0 {
						if engine.catalog.Skills == nil {
							engine.catalog.Skills = map[int][]CombatSkillDefinition{}
							engine.catalog.SkillRoles = map[int][]CombatSkillRole{}
						}
						registerNativeActionSkill(t, sphere.Support, engine.catalog.Skills, engine.catalog.SkillRoles)
					}
				}
				for _, buddy := range p.Buddies {
					definition, err := battlecontent.ParseBuddy(buddy.CSVRow)
					if err != nil || buddy.Slot < 1 || buddy.Slot > 5 {
						t.Fatalf("invalid original Buddy: %v", err)
					}
					if engine.catalog.Buddies == nil {
						engine.catalog.Buddies = map[int]CombatBuddyDefinition{}
					}
					engine.catalog.Buddies[definition.ID] = definition
					player.Buddies[buddy.Slot-1] = BattleBuddy{BuddyType: buddy.Slot, BuddyID: definition.ID, Level: buddy.Level}
					if buddy.Slot == 1 {
						player.BurstState, player.Burst = burstGaugeNormal, specimen.Input.BurstInitial
					}
					skills := []nativeActionAPISkill{buddy.Passive}
					for _, slot := range buddy.Skills {
						skills = append(skills, slot.Skill, slot.Call)
					}
					for _, definition := range skills {
						registerNativeActionSkill(t, definition, engine.catalog.Skills, engine.catalog.SkillRoles)
					}
				}
			}
			if len(specimen.Input.Enemies) != 0 {
				partyID, drops := nativeActionAPIParty(t, engine, specimen.Input.OfficialPartyRow, specimen.Input.Enemies)
				if err := engine.loadEnemyParty(engine.catalog.EnemyParties[partyID], drops); err != nil {
					t.Fatal(err)
				}
			} else {
				enemy := &engine.enemies[0]
				enemy.HP, enemy.MaxHP, enemy.BaseMaxHP = specimen.Input.EnemyHP, specimen.Input.EnemyHP, specimen.Input.EnemyHP
				enemy.Attack, enemy.BaseAttack = 100, 100
				enemy.Level.ActionsPerTurn = len(specimen.Input.EnemySkill)
				for i, es := range specimen.Input.EnemySkill {
					s, _ := es.definitions()
					registerNativeActionSkill(t, es, engine.catalog.Skills, engine.catalog.SkillRoles)
					enemy.Level.Actions = append(enemy.Level.Actions, CombatEnemyAction{Slot: i + 1, SkillID: s.ID, Category: "skill",
						Target: "RANDOM", ActionCost: 1, MaxUses: 99, Rate: 100, Priority: es.Priority})
				}
			}
			for phaseIndex, phase := range specimen.Phases {
				var rows []BattleResult
				var err error
				switch phase.Name {
				case "resume_data_get":
					var haveDrops []BattleDrop
					for _, drop := range phase.HaveDrops {
						haveDrops = append(haveDrops, BattleDrop{RewardType: drop[0], Num: drop[1], RewardTypeID: drop[2]})
					}
					rows, err = engine.ResumeResults(haveDrops...)
				case "start":
					if phase.Wave > 0 {
						if phase.Wave > len(specimen.Input.Waves) {
							t.Fatal("unknown next wave", phase.Wave)
						}
						wave := specimen.Input.Waves[phase.Wave-1]
						if len(wave.Enemies) != 0 {
							partyID, drops := nativeActionAPIParty(t, engine, wave.OfficialPartyRow, wave.Enemies)
							engine, err = engine.NextBattle(partyID, drops)
						} else {
							definition := engine.catalog.Enemies[1]
							definition.HP, definition.Attack = wave.EnemyHP, 100
							engine.catalog.Enemies[1] = definition
							level := engine.catalog.EnemyLevels[1]
							level.ActionsPerTurn, level.Actions = len(wave.EnemySkill), nil
							for i, es := range wave.EnemySkill {
								s, _ := es.definitions()
								registerNativeActionSkill(t, es, engine.catalog.Skills, engine.catalog.SkillRoles)
								level.Actions = append(level.Actions, CombatEnemyAction{Slot: i + 1, SkillID: s.ID, Category: "skill",
									Target: "RANDOM", ActionCost: 1, MaxUses: 99, Rate: 100, Priority: es.Priority})
							}
							engine.catalog.EnemyLevels[1] = level
							engine, err = engine.NextBattle(1, nil)
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					rows, err = engine.Start()
				case "turn_phase":
					rows, err = engine.TurnPhase()
				case "user_phase":
					rows, err = engine.UserPhase()
				case "user_card_play":
					rows, err = engine.Submit(phase.Submission.Member, cardPlaySubmission{CardTypes: phase.Submission.Cards, Targets: phase.Submission.Targets,
						SphereSlot: phase.Submission.SphereSlot, SphereTarget: phase.Submission.SphereTarget})
				case "chalice_sphr_reserve":
					rows, err = engine.ReserveChaliceSphere(phase.Reservation.Member, phase.Reservation.Slot)
				case "burst_skill_exec":
					rows, err = engine.ExecuteBurst(phase.Burst.Member, burstSkillSubmission{Target: phase.Burst.Target, CardTypes: phase.Burst.Cards, SkillSlot: phase.Burst.Slot})
				case "user_attack":
					rows, err = engine.UserAttack()
				case "chalice_sphr_exec_user_phase":
					rows, err = engine.ExecuteChaliceUserPhase()
				case "enemy_phase":
					rows, err = engine.EnemyPhase()
				case "chalice_sphr_exec_enemy_phase":
					rows, err = engine.ExecuteChaliceEnemyPhase()
				default:
					t.Fatal("unknown original phase", phase.Name)
				}
				if err != nil {
					t.Fatalf("phase %d/%s: %v", phaseIndex, phase.Name, err)
				}
				// Original API receipts contain engine rows, before the server's
				// transport adapter separates damage from local retirement with 502.
				// Compare the original layer directly; the wire boundary is covered
				// by TestRetirementWireWaitsForDamagePresentation.
				encoded := make([]string, 0, len(rows))
				for _, row := range rows {
					text, encodeErr := encodeBattleResult(row)
					if encodeErr != nil {
						t.Fatal(encodeErr)
					}
					encoded = append(encoded, text)
				}
				actual := strings.Join(encoded, "\n")
				expected := phase.CSV
				if phase.Name == "start" {
					actual, err = testStartResult(members, rows)
					if err != nil {
						t.Fatal(err)
					}
					expected = strings.TrimPrefix(expected, "999,105,0\n")
				}
				a, b := strings.Split(strings.TrimSpace(actual), "\n"), strings.Split(strings.TrimSpace(expected), "\n")
				for i := 0; i < maxInt(len(a), len(b)); i++ {
					if i >= len(a) || i >= len(b) {
						t.Fatalf("phase %d/%s row count: Go=%d native=%d\nGo:\n%s\nnative:\n%s", phaseIndex, phase.Name, len(a), len(b), actual, expected)
					}
					if a[i] != b[i] {
						t.Fatalf("phase %d/%s row %d: Go=%s native=%s\nGo:\n%s\nnative:\n%s", phaseIndex, phase.Name, i, a[i], b[i], actual, expected)
					}
				}
				if rng := [4]uint32{engine.rng.x, engine.rng.y, engine.rng.z, engine.rng.w}; rng != phase.RNG {
					t.Fatalf("phase %d/%s RNG: Go=%v native=%v", phaseIndex, phase.Name, rng, phase.RNG)
				}
			}
			if specimen.ScoreDamage != nil && engine.ScoreDamage() != *specimen.ScoreDamage {
				t.Fatalf("score damage: Go=%d native=%d", engine.ScoreDamage(), *specimen.ScoreDamage)
			}
		})
	}
}
