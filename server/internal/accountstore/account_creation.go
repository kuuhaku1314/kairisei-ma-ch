package accountstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

const (
	CreationDisabled     = "account_creation_disabled"
	CreationDailyLimit   = "account_creation_daily_limit"
	CreationRateLimited  = "account_creation_rate_limited"
	creationAttemptLimit = 12
	creationAddressLimit = 4096
)

var creationLocation = time.FixedZone("UTC+8", 8*60*60)

// CreationRejection is a business refusal, distinct from storage or input errors.
type CreationRejection struct {
	Code              string
	RetryAfterSeconds int
}

func (rejection *CreationRejection) Error() string {
	switch rejection.Code {
	case CreationDisabled:
		return "当前暂停创建新账号，请登录已有账号。"
	case CreationDailyLimit:
		return "当前网络今日创建账号数量已达上限，请于北京时间次日00:00后重试，或登录已有账号。"
	default:
		return "当前网络创建账号尝试过于频繁，请稍后重试，或登录已有账号。"
	}
}

type creationGuard struct {
	policy     func() CreationPolicy
	signingKey [32]byte
	now        func() time.Time
	mu         sync.Mutex
	attempts   map[netip.Addr][]time.Time
	lastSweep  time.Time
	rejected   [3]atomic.Uint64
}

type creationAdmission struct {
	guard    *creationGuard
	source   netip.Addr
	uuid     string
	proof    CreationProof
	verified *verifiedCreationProof
}

type CreationPolicy struct {
	DailyLimit       int
	VerificationBits int
	LoginWhitelist   LoginWhitelist
}

func (policy CreationPolicy) difficulty() (byte, error) {
	if policy.DailyLimit < 0 || policy.DailyLimit > 10000 {
		return 0, errors.New("invalid account creation daily limit")
	}
	if policy.VerificationBits != 0 && (policy.VerificationBits < 16 || policy.VerificationBits > 32) {
		return 0, errors.New("invalid account creation verification bits")
	}
	return byte(policy.VerificationBits), nil
}

// AttachCreationPolicy is startup-only. The getter must read an immutable atomic
// publication, without database access or an Admin configuration lock.
func (accounts *Accounts) AttachCreationPolicy(policy func() CreationPolicy) error {
	if policy == nil {
		return errors.New("account creation policy is required")
	}
	if _, err := policy().difficulty(); err != nil {
		return err
	}
	guard := &creationGuard{policy: policy, now: time.Now, attempts: make(map[netip.Addr][]time.Time)}
	if _, err := rand.Read(guard.signingKey[:]); err != nil {
		return fmt.Errorf("generate account creation signing key: %w", err)
	}
	if !accounts.creation.CompareAndSwap(nil, guard) {
		return errors.New("account creation policy is already attached")
	}
	return nil
}

// ValidateCreationPolicy prevents exposing public routes before policy wiring.
func (accounts *Accounts) ValidateCreationPolicy() error {
	guard := accounts.creation.Load()
	if guard == nil {
		return errors.New("account creation policy is not attached")
	}
	_, err := guard.policy().difficulty()
	return err
}

// CreationRejections exposes process-lifetime aggregates, never an IP history.
func (accounts *Accounts) CreationRejections() map[string]uint64 {
	counts := make(map[string]uint64, 3)
	if guard := accounts.creation.Load(); guard != nil {
		for index, code := range []string{CreationDisabled, CreationDailyLimit, CreationRateLimited} {
			counts[code] = guard.rejected[index].Load()
		}
	}
	return counts
}

func (admission *creationAdmission) record(err error) {
	var rejection *CreationRejection
	if !errors.As(err, &rejection) {
		return
	}
	for index, code := range []string{CreationDisabled, CreationDailyLimit, CreationRateLimited} {
		if rejection.Code == code {
			admission.guard.rejected[index].Add(1)
			return
		}
	}
}

func (accounts *Accounts) clientAdmission(source netip.Addr, proof CreationProof) (*creationAdmission, error) {
	if !source.IsValid() {
		return nil, errors.New("account creation source IP is invalid")
	}
	if len(proof.Challenge) > 100 || len(proof.Solution) > 16 {
		return nil, errors.New("account creation proof exceeds input limits")
	}
	guard := accounts.creation.Load()
	if guard == nil {
		return nil, errors.New("account creation policy is not attached")
	}
	return &creationAdmission{guard: guard, source: source.Unmap().WithZone(""), proof: proof}, nil
}

func (accounts *Accounts) ResolveClientLogin(ctx context.Context, uuid string, source netip.Addr, proof CreationProof) (identity accountIdentity, err error) {
	admission, err := accounts.clientAdmission(source, proof)
	if err != nil {
		return identity, err
	}
	defer func() { err = admission.finish(ctx, err); admission.record(err) }()
	return accounts.resolveLogin(ctx, uuid, admission)
}

func (accounts *Accounts) BindClientAccount(ctx context.Context, uuid, name, password string, source netip.Addr, proof CreationProof) (binding AccountBinding, err error) {
	admission, err := accounts.clientAdmission(source, proof)
	if err != nil {
		return binding, err
	}
	defer func() { err = admission.finish(ctx, err); admission.record(err) }()
	return accounts.bindAccount(ctx, uuid, name, password, admission)
}

// One statement observes the identity and its source's daily count in the same
// read snapshot. No read transaction or writer lease survives this precheck.
func (accounts *Accounts) precheckCreation(ctx context.Context, uuid string, admission *creationAdmission) error {
	admission.uuid = uuid
	reader, err := accounts.storage.OpenRead()
	if err != nil {
		return err
	}
	guard := admission.guard
	var attemptErr error
	for pass := 0; pass < 2; pass++ {
		now := guard.now()
		day := now.In(creationLocation).Format(time.DateOnly)
		var known bool
		var count int
		if err := reader.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM cn_local_account WHERE login_uuid = ?),
 COALESCE((SELECT created_count FROM cn_account_creation_daily WHERE day = ? AND source_ip = ?), 0)`,
			uuid, day, admission.source.String()).Scan(&known, &count); err != nil {
			return fmt.Errorf("precheck account creation: %w", err)
		}
		if known {
			return nil
		}
		if pass == 0 {
			attemptErr = guard.allowAttempt(admission.source)
		}
		decisionTime := guard.now()
		policy := guard.policy()
		if !policy.LoginWhitelist.Allows(0) {
			return ErrLoginWhitelist
		}
		err := attemptErr
		if err == nil {
			err = creationQuotaRejection(policy.DailyLimit, count, decisionTime)
			if err == nil {
				err = admission.precheckProof(policy, decisionTime)
			}
		}
		if err == nil || pass == 1 || decisionTime.In(creationLocation).Format(time.DateOnly) == day {
			return err
		}
	}
	return nil
}

func creationQuotaRejection(limit, count int, now time.Time) error {
	if limit == 0 {
		return &CreationRejection{Code: CreationDisabled}
	}
	if limit < 0 || limit > 10000 {
		return errors.New("invalid account creation daily limit")
	}
	if count >= limit {
		local := now.In(creationLocation)
		midnight := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, creationLocation)
		return &CreationRejection{Code: CreationDailyLimit, RetryAfterSeconds: creationRetrySeconds(midnight.Sub(now))}
	}
	return nil
}

func creationRetrySeconds(wait time.Duration) int {
	seconds := int((wait + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func pruneCreationAttempts(times []time.Time, now time.Time) []time.Time {
	cutoff := now.Add(-time.Minute)
	for len(times) > 0 && !times[0].After(cutoff) {
		times = times[1:]
	}
	return times
}

func (guard *creationGuard) allowAttempt(source netip.Addr) error {
	guard.mu.Lock()
	defer guard.mu.Unlock()
	// Capture admission time under the same lock as append, so concurrent read
	// prechecks cannot insert timestamps out of order or age while awaiting SQL.
	now := guard.now()
	if guard.lastSweep.IsZero() || now.Sub(guard.lastSweep) >= time.Minute {
		for address, times := range guard.attempts {
			times = pruneCreationAttempts(times, now)
			if len(times) == 0 {
				delete(guard.attempts, address)
			} else {
				guard.attempts[address] = times
			}
		}
		guard.lastSweep = now
	}
	times := pruneCreationAttempts(guard.attempts[source], now)
	if len(times) == 0 {
		delete(guard.attempts, source)
	}
	if len(times) >= creationAttemptLimit {
		guard.attempts[source] = times
		return &CreationRejection{Code: CreationRateLimited, RetryAfterSeconds: creationRetrySeconds(times[0].Add(time.Minute).Sub(now))}
	}
	if len(times) == 0 && len(guard.attempts) >= creationAddressLimit {
		return &CreationRejection{Code: CreationRateLimited, RetryAfterSeconds: creationRetrySeconds(guard.lastSweep.Add(time.Minute).Sub(now))}
	}
	guard.attempts[source] = append(times, now)
	return nil
}

// Called only after BEGIN IMMEDIATE and the final identity lookup. Claim and
// retention work roll back together with the identity, seed and credentials.
func (admission *creationAdmission) claim(ctx context.Context, tx *sql.Tx) error {
	now := admission.guard.now()
	policy := admission.guard.policy()
	if !policy.LoginWhitelist.Allows(0) {
		return ErrLoginWhitelist
	}
	limit := policy.DailyLimit
	if err := creationQuotaRejection(limit, 0, now); err != nil {
		return err
	}
	day := now.In(creationLocation).Format(time.DateOnly)
	if err := admission.checkFinalProof(policy, now); err != nil {
		// A quota filled since precheck takes priority over a new proof challenge.
		// This extra read is needed only on an expired or newly insufficient proof.
		var count int
		if readErr := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT created_count FROM cn_account_creation_daily WHERE day = ? AND source_ip = ?), 0)`, day, admission.source.String()).Scan(&count); readErr != nil {
			return readErr
		}
		if quotaErr := creationQuotaRejection(limit, count, now); quotaErr != nil {
			return quotaErr
		}
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO cn_account_creation_daily(day, source_ip, created_count)
 VALUES (?, ?, 1) ON CONFLICT(day, source_ip) DO UPDATE SET created_count = created_count + 1
 WHERE created_count < ?`, day, admission.source.String(), limit)
	if err != nil {
		return fmt.Errorf("claim account creation quota: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return creationQuotaRejection(limit, limit, now)
	}
	if affected != 1 {
		return fmt.Errorf("unexpected account creation quota row count: %d", affected)
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM cn_account_creation_daily WHERE (day, source_ip) IN (
 SELECT day, source_ip FROM cn_account_creation_daily WHERE day < ? ORDER BY day, source_ip LIMIT 128
)`, now.In(creationLocation).AddDate(0, 0, -6).Format(time.DateOnly))
	return err
}
