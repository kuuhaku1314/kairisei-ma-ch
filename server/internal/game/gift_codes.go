package game

import (
	"errors"
	"strings"
	"time"

	"kairisei.local/server/internal/gamestate"
)

type GiftCodeBatch struct {
	ID         int                `json:"id"`
	Revision   int                `json:"revision"`
	Name       string             `json:"name"`
	Mode       string             `json:"mode"` // public (one use per account) or single (one use globally)
	Title      string             `json:"title"`
	Message    string             `json:"message"`
	ClaimScope string             `json:"claim_scope"` // batch, group, or code
	Group      string             `json:"group"`       // Only used by the group claim scope.
	Limit      int                `json:"limit"`       // Zero means unlimited total redemptions.
	StartsAt   int64              `json:"starts_at"`
	EndsAt     int64              `json:"ends_at"`
	Enabled    bool               `json:"enabled"`
	Published  bool               `json:"published"`
	Rewards    []gamestate.Reward `json:"rewards"`
}

type GiftCodeOffer struct {
	CodeID int64
	Batch  GiftCodeBatch
}

type GiftCodeRepository interface {
	PrepareGiftCode(userID int, code string, now time.Time) (GiftCodeOffer, int, error)
	CommitGiftCode(offer GiftCodeOffer, state gamestate.State, presentIDs []int64, now time.Time) (int, error)
}

func NormalizeGiftCode(code string) (string, error) {
	code = strings.TrimSpace(code)
	if len(code) < 6 || len(code) > 64 {
		return "", errors.New("兑换码须为6–64位字母、数字或连字符")
	}
	for _, c := range code {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return "", errors.New("兑换码只能使用ASCII字母、数字和连字符")
		}
	}
	return strings.ToUpper(code), nil
}

func (s *Account) PrepareGiftPresents(batch GiftCodeBatch) ([]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(batch.Rewards) == 0 {
		return nil, errors.New("兑换批次没有奖励")
	}
	for _, r := range batch.Rewards {
		if err := s.validateRewardLocked(r); err != nil {
			return nil, err
		}
	}
	ids := make([]int64, 0, len(batch.Rewards))
	for _, r := range batch.Rewards {
		s.appendRewardPresentLocked(r, batch.Title, batch.Message)
		ids = append(ids, s.presents[len(s.presents)-1].PresentID)
	}
	return ids, nil
}
