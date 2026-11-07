// kairi-inventory-audit checks isolated import candidates with production
// combat validators. It does not start a server or open an account database.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"kairisei.local/server/internal/multiplayer"
)

type item struct {
	Kind    string   `json:"kind"`
	ID      int      `json:"id"`
	Reasons []string `json:"reasons"`
	Errors  []string `json:"errors"`
}

func run() error {
	card := flag.String("card-master", "", "isolated projected card CSV")
	battle := flag.String("battle-master-root", "", "isolated projected combat tables")
	input := flag.String("inventory", "", "candidate inventory.json")
	output := flag.String("output", "", "new report path")
	flag.Parse()
	data, err := os.ReadFile(*input)
	if err != nil {
		return err
	}
	var manifest struct {
		Decisions []item `json:"decisions"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	catalog, err := multiplayer.LoadCombatCatalog(*card, *battle)
	if err != nil {
		return err
	}
	var checked []item
	for _, entry := range manifest.Decisions {
		if len(entry.Reasons) != 0 {
			continue
		}
		c := *catalog
		c.Cards = map[int]multiplayer.CombatCardDefinition{}
		c.Spheres = map[int]multiplayer.CombatSphereDefinition{}
		c.Buddies = map[int]multiplayer.CombatBuddyDefinition{}
		checks := []func() error{}
		switch entry.Kind {
		case "card":
			v, ok := catalog.Cards[entry.ID]
			if !ok {
				return fmt.Errorf("missing candidate card %d", entry.ID)
			}
			c.Cards[entry.ID] = v
			checks = append(checks, c.ValidatePlayerFunctionCoverage, c.ValidateCardSupportFunctionCoverage)
		case "sphere":
			v, ok := catalog.Spheres[entry.ID]
			if !ok {
				return fmt.Errorf("missing candidate sphere %d", entry.ID)
			}
			c.Spheres[entry.ID] = v
			checks = append(checks, c.ValidatePlayerFunctionCoverage)
			if v.PassiveSkillID != 0 {
				checks = append(checks, c.ValidateSphereSupportFunctionCoverage)
			}
		case "buddy":
			v, ok := catalog.Buddies[entry.ID]
			if !ok {
				return fmt.Errorf("missing candidate buddy %d", entry.ID)
			}
			c.Buddies[entry.ID] = v
			c.Skills = map[int][]multiplayer.CombatSkillDefinition{}
			c.SkillRoles = map[int][]multiplayer.CombatSkillRole{}
			ids := []int{v.PassiveSkillID}
			for _, skill := range v.Skills {
				ids = append(ids, skill.SkillID, skill.CallSkillID)
			}
			for _, id := range ids {
				if id == 0 {
					continue
				}
				c.Skills[id] = catalog.Skills[id]
				for _, skill := range c.Skills[id] {
					c.SkillRoles[skill.FunctionID] = catalog.SkillRoles[skill.FunctionID]
				}
			}
			checks = append(checks, c.ValidateBurstFunctionCoverage)
		default:
			return fmt.Errorf("unknown inventory kind %q", entry.Kind)
		}
		entry.Errors = []string{}
		for _, check := range checks {
			if err := check(); err != nil {
				entry.Errors = append(entry.Errors, err.Error())
			}
		}
		sort.Strings(entry.Errors)
		checked = append(checked, entry)
	}
	data, err = json.MarshalIndent(struct {
		Items []item `json:"items"`
	}{checked}, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
