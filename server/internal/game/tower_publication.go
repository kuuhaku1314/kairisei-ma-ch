package game

import (
	"maps"
	"time"
)

// TowerAdmission is operator policy, not player progress or resource identity.
// A nil allowlist opens every tower; a non-nil list opens only selected towers,
// matching ordinary Boss publication. Source-region flags are not runtime gates.
type TowerAdmission struct {
	TowerIDs  map[int]struct{}
	StartUnix int64
	EndUnix   int64
}

func (policy TowerAdmission) Clone() TowerAdmission {
	policy.TowerIDs = maps.Clone(policy.TowerIDs)
	return policy
}

func (policy TowerAdmission) Allows(towerID int, now int64) bool {
	if now < policy.StartUnix || policy.EndUnix != 0 && now >= policy.EndUnix {
		return false
	}
	if policy.TowerIDs == nil {
		return true
	}
	_, enabled := policy.TowerIDs[towerID]
	return enabled
}

type TowerAdmissionConfigurator interface {
	ApplyTowerAdmission(TowerAdmission)
}

func (s *Account) ApplyTowerAdmission(policy TowerAdmission) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.towerAdmission = policy.Clone()
}

func (s *Account) towerOpenLocked(towerID int) bool {
	_, found := s.towerQuestProfiles[towerID]
	return found && s.towerAdmission.Allows(towerID, time.Now().Unix())
}

// The result-report path still uses TowerQuestHasBoss, which ignores the live
// operator switch. A room started before closure must remain settleable.
func (s *Account) TowerQuestBossAdmission(bossID int) (known, open bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tower, _, found := s.towerQuestBattleForBossLocked(bossID)
	return found, found && s.towerOpenLocked(tower.TowerID)
}
