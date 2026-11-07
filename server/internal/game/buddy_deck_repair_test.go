package game

import (
	"reflect"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestEquippedMaterialCannotBeConsumed(t *testing.T) {
	for _, kind := range []string{"sphere", "buddy"} {
		for _, operation := range []string{"fusion", "evolution", "sell"} {
			t.Run(kind+"/"+operation, func(t *testing.T) {
				s := ConsumptionTestStore(t)
				s.spheres = []gamestate.Sphere{{UniqueID: 1, SphereID: 100, Level: 1, BaseAddPrice: 100}, {UniqueID: 2, SphereID: 100, Level: 1}}
				s.sphereDefinitions = map[int]gamestate.SphereDefinition{
					100: {SphereID: 100, SameSphereID: 100, MaxLevel: 2, ExperienceTableID: 1, EvolutionID: 101},
					101: {SphereID: 101, SameSphereID: 100, MaxLevel: 3, ExperienceTableID: 1, EvolutionCount: 1, Rarity: "MILLIONRARE"},
				}
				s.sphereEvoPrices = map[string][]int{"MILLIONRARE": {100}}
				s.buddies = []gamestate.Buddy{{UniqueID: 1, BuddyID: 100, Level: 1, BaseAddPrice: 100}, {UniqueID: 2, BuddyID: 100, Level: 1}}
				s.buddyDefinitions = map[int]gamestate.BuddyDefinition{
					100: {BuddyID: 100, SameBuddyID: 100, MaxLevel: 2, ExperienceTableID: 1, EvolutionID: 101, Rarity: "MILLIONRARE"},
					101: {BuddyID: 101, SameBuddyID: 100, MaxLevel: 3, ExperienceTableID: 1, EvolutionCount: 1},
				}
				s.buddyEvoPrices = map[string][]int{"MILLIONRARE": {100}}
				s.buddyExperience = map[int][]int{1: {10000, 10000}}
				s.buddyProgression.MaximumMaterialCount = 100
				s.decks = []DeckInfo{{ArthurType: 5, CardUniqueIDs: []int64{2}, SphereUniqueIDs: []int64{2}, BuddyUniqueIDs: []int64{1, 2}}}
				beforeSpheres := append([]gamestate.Sphere(nil), s.spheres...)
				beforeBuddies := append([]gamestate.Buddy(nil), s.buddies...)
				beforeDecks := CloneDecks(s.decks)
				var err error
				want := errSphereInDeck
				switch kind {
				case "sphere":
					switch operation {
					case "fusion":
						_, _, _, _, _, err = s.FuseSphere(1, []SphereFusionInput{{InputType: 0, ID: 2, Num: 1}})
					case "evolution":
						_, _, _, _, err = s.EvolveSphere(1, 2, 0)
					case "sell":
						_, _, _, _, err = s.SellSpheres([]int64{2})
					}
				case "buddy":
					want = errBuddyInDeck
					switch operation {
					case "fusion":
						_, err = s.FuseBuddy(1, []BuddyFusionInput{{InputType: 2, ID: 2, Num: 1}})
					case "evolution":
						_, err = s.EvolveBuddy(1, 2, 0)
					case "sell":
						_, _, _, _, err = s.SellBuddies([]int64{2})
					}
				}
				if err != want || !reflect.DeepEqual(s.spheres, beforeSpheres) || !reflect.DeepEqual(s.buddies, beforeBuddies) || !reflect.DeepEqual(CloneDecks(s.decks), beforeDecks) || s.gold != 100000 {
					t.Fatalf("equipped material was not rejected atomically: %v", err)
				}
				// The same number in a different inventory must not block this item.
				if kind == "sphere" {
					s.decks[0].SphereUniqueIDs[0] = 0
					_, _, _, _, err = s.SellSpheres([]int64{2})
				} else {
					s.decks[0].BuddyUniqueIDs[1] = 0
					_, _, _, _, err = s.SellBuddies([]int64{2})
				}
				if err != nil || s.decks[0].CardUniqueIDs[0] != 2 {
					t.Fatalf("unrelated equipped UID blocked sale: %v", err)
				}
			})
		}
	}
}
