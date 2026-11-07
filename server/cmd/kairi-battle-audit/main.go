package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"kairisei.local/server/internal/multiplayer"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("kairi-battle-audit", flag.ContinueOnError)
	cardMaster := flags.String("cn-combat-card-master", "", "configured card.csv")
	battleMasterRoot := flags.String("cn-combat-master-root", "", "configured battle master directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *cardMaster == "" || *battleMasterRoot == "" {
		return errors.New("-cn-combat-card-master and -cn-combat-master-root are required")
	}
	catalog, err := multiplayer.LoadCombatCatalog(*cardMaster, *battleMasterRoot)
	if err != nil {
		return err
	}
	if err := catalog.ValidatePlayerFunctionCoverage(); err != nil {
		return err
	}
	if err := catalog.ValidateBurstFunctionCoverage(); err != nil {
		return err
	}
	if err := catalog.ValidateSphereSupportFunctionCoverage(); err != nil {
		return err
	}
	if err := catalog.ValidateEnemyFunctionCoverage(); err != nil {
		return err
	}
	if err := catalog.ValidateRoleParameterContracts(); err != nil {
		return err
	}
	if err := multiplayer.ValidateBattleEngineContracts(catalog); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"state":           "PASS",
		"source":          "configured-battle-and-card-master",
		"mode":            "static-contract-and-memory-simulation",
		"cards":           len(catalog.Cards),
		"spheres":         len(catalog.Spheres),
		"buddies":         len(catalog.Buddies),
		"skills":          len(catalog.Skills),
		"skill_role_sets": len(catalog.SkillRoles),
		"parameter_rules": len(catalog.RoleParamRules),
	})
}
