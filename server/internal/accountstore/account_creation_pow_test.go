package accountstore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// Test-only deterministic key, nonce and signed tokens. No runtime key is exported.
var creationPowVectors = []struct {
	difficulty byte
	nonce      string
	challenge  string
	solution   string
	digest     string
}{
	{18, "101112131415161718191a1b1c1d1e1f", "AQESAAAAAGrqBXgBI0VniatM3o-rASNFZ4mrEBESExQVFhcYGRobHB0eH2HTCqHngnUhCmOA_g7xhNemEdXjPFd_-sNqHyNnM1nW", "000000000009c745", "0000144eba96df9318c73ef245dd45cf5a472d24eb9d342750303831fe9b2bc4"},
	{20, "202122232425262728292a2b2c2d2e2f", "AQEUAAAAAGrqBXgBI0VniatM3o-rASNFZ4mrICEiIyQlJicoKSorLC0uL_VCdWRkF7HiB0HBli7YM6pIORQEVEMdp2RPY6WvDsVk", "000000000009c91b", "00000637dd4b8c443a2d577cacdf651a9932f546e6f0b512a44607b79662e9d9"},
	{19, "303132333435363738393a3b3c3d3e3f", "AQETAAAAAGrqBXgBI0VniatM3o-rASNFZ4mrMDEyMzQ1Njc4OTo7PD0-P9O3HEoRzMW1uknTGCQiXo7nSgG4yPTTC_v8AOyagfOe", "000000000003d036", "000001b6d93161b3098e02de2cb19abc737001650c7e2511a933e9c64ca1da01"},
}

const creationPowUUID = "01234567-89ab-4cde-8fab-0123456789ab"
const creationPowNow int64 = 1793721600

func fixedCreationPowGuard() *creationGuard {
	guard := &creationGuard{now: func() time.Time { return time.Unix(creationPowNow, 0) }}
	for index := range guard.signingKey {
		guard.signingKey[index] = byte(index)
	}
	return guard
}

func attachPowPolicy(t *testing.T, accounts *Accounts, getter func() CreationPolicy) *creationGuard {
	t.Helper()
	if err := accounts.AttachCreationPolicy(getter); err != nil {
		t.Fatal(err)
	}
	guard := accounts.creation.Load()
	guard.signingKey = fixedCreationPowGuard().signingKey
	guard.now = func() time.Time { return time.Unix(creationPowNow, 0) }
	return guard
}

func requireCreationChallenge(t *testing.T, err error, difficulty int) CreationChallenge {
	t.Helper()
	var required *CreationChallengeRequired
	if !errors.As(err, &required) || required.Verification.Difficulty != difficulty {
		t.Fatalf("expected %d-bit challenge, got %v", difficulty, err)
	}
	challenge := required.Verification
	if challenge.Algorithm != "sha256-prefix-v1" || len(challenge.Challenge) != 100 {
		t.Fatal("invalid challenge shape", challenge)
	}
	return challenge
}

func fixedCreationProof(index int) CreationProof {
	vector := creationPowVectors[index]
	return CreationProof{Challenge: vector.challenge, Solution: vector.solution}
}

func TestCreationPowFixedBytesAndAuthenticatedFields(t *testing.T) {
	guard := fixedCreationPowGuard()
	identity, err := creationUUIDBytes(creationPowUUID)
	if err != nil {
		t.Fatal(err)
	}
	for index, vector := range creationPowVectors {
		var nonce [16]byte
		decoded, _ := hex.DecodeString(vector.nonce)
		copy(nonce[:], decoded)
		if got := encodeCreationChallenge(guard.signingKey, identity, vector.difficulty, creationPowNow+120, nonce); got != vector.challenge {
			t.Fatal("signed layout differs from independent Python vector", got)
		}
		proof := fixedCreationProof(index)
		digest := sha256.Sum256([]byte(proof.Challenge + ":" + proof.Solution))
		if hex.EncodeToString(digest[:]) != vector.digest || !creationDigestMatches(digest, vector.difficulty) {
			t.Fatal("UTF-8 prefix digest mismatch")
		}
		if guard.verifyProof(creationPowUUID, proof, time.Unix(creationPowNow+119, 0)) == nil {
			t.Fatal("valid vector rejected")
		}
		if guard.verifyProof(creationPowUUID, proof, time.Unix(creationPowNow+120, 0)) != nil {
			t.Fatal("expiry boundary accepted")
		}
	}
	proof := fixedCreationProof(0)
	if guard.verifyProof(creationPrimaryUUID, proof, guard.now()) != nil {
		t.Fatal("cross-UUID proof accepted")
	}
	// Re-sign invalid fields, so the tests reach version/purpose/difficulty checks.
	for _, field := range []struct {
		offset int
		value  byte
	}{{0, 2}, {1, 2}, {2, 15}, {2, 33}} {
		token, _ := base64.RawURLEncoding.DecodeString(proof.Challenge)
		token[field.offset] = field.value
		mac := hmac.New(sha256.New, guard.signingKey[:])
		_, _ = mac.Write(token[:43])
		copy(token[43:], mac.Sum(nil))
		changed := CreationProof{Challenge: base64.RawURLEncoding.EncodeToString(token), Solution: proof.Solution}
		if guard.verifyProof(creationPowUUID, changed, guard.now()) != nil {
			t.Fatal("authenticated unsupported token field accepted", field)
		}
	}
	for _, changed := range []CreationProof{
		{Challenge: proof.Challenge[:99] + "!", Solution: proof.Solution},
		{Challenge: proof.Challenge[:99] + "A", Solution: proof.Solution},
		{Challenge: proof.Challenge + "=", Solution: proof.Solution},
		{Challenge: proof.Challenge, Solution: strings.ToUpper(proof.Solution)},
		{Challenge: proof.Challenge, Solution: "0000000000000000"},
		{Challenge: proof.Challenge, Solution: "000000000009c74"},
	} {
		if guard.verifyProof(creationPowUUID, changed, guard.now()) != nil {
			t.Fatal("malformed, unsigned or incorrect proof accepted")
		}
	}
	for _, test := range []struct {
		high byte
		bits byte
		want bool
	}{{0xff, 16, true}, {0x7f, 17, true}, {0x80, 17, false}, {0x3f, 18, true}, {0x40, 18, false}, {0x1f, 19, true}, {0x20, 19, false}, {0x0f, 20, true}, {0x10, 20, false}, {0x07, 21, true}, {0x08, 21, false}, {0, 24, true}, {1, 24, false}, {0, 32, true}, {0, 0, false}, {0, 15, false}, {0, 33, false}} {
		var digest [32]byte
		digest[2] = test.high
		if creationDigestMatches(digest, test.bits) != test.want {
			t.Fatal("high-bit boundary mismatch", test)
		}
	}
	if digest := [32]byte{3: 1}; !creationDigestMatches(digest, 31) || creationDigestMatches(digest, 32) {
		t.Fatal("fourth-byte boundary mismatch")
	}
}

func TestCreationPowCustomBitsAndSingleIdentity(t *testing.T) {
	accounts := creationTestAccounts(t)
	policy := CreationPolicy{DailyLimit: 2, VerificationBits: 19}
	attachPowPolicy(t, accounts, func() CreationPolicy { return policy })
	source := netip.MustParseAddr("192.0.2.85")
	proof := fixedCreationProof(2)
	if _, err := accounts.ResolveClientLogin(context.Background(), creationPowUUID, source, proof); err != nil {
		t.Fatal("custom proof failed", err)
	}
	policy.VerificationBits = 32
	if _, err := accounts.ResolveClientLogin(context.Background(), creationPowUUID, source, proof); err != nil || creationCount(t, accounts) != 1 {
		t.Fatal("successful retry did not reuse the same identity", err)
	}
	_, err := accounts.ResolveClientLogin(context.Background(), "01234567-89ab-4cde-8fab-0123456789ac", source, proof)
	// Validity follows the client budget: 30 minutes at the cap, plus 2 for the network.
	if challenge := requireCreationChallenge(t, err, 32); challenge.ExpiresAtUnix != creationPowNow+32*60 {
		t.Fatal("challenge validity does not cover the solve budget", challenge.ExpiresAtUnix-creationPowNow)
	}
	if creationCount(t, accounts) != 1 {
		t.Fatal("cross-UUID proof consumed quota")
	}
}

func TestCreationPowPrecheckPriorityAndKnownIdentity(t *testing.T) {
	accounts := creationTestAccounts(t)
	policy := CreationPolicy{DailyLimit: 1, VerificationBits: 18}
	guard := attachPowPolicy(t, accounts, func() CreationPolicy { return policy })
	source := netip.MustParseAddr("192.0.2.81")
	db, _ := accounts.storage.Open()
	blocker, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	waits := db.Stats().WaitCount
	_, err = accounts.BindClientAccount(context.Background(), creationPowUUID, "pow_player", "valid-password", source, CreationProof{})
	challenge := requireCreationChallenge(t, err, 18)
	if challenge.ExpiresAtUnix != creationPowNow+240 || creationCount(t, accounts) != 0 || db.Stats().WaitCount != waits {
		t.Fatal("challenge consumed quota or waited for writer")
	}
	if binding, err := accounts.AccountBinding(creationPowUUID); err != nil || binding.UserID != 0 {
		t.Fatal("challenge created identity", binding, err)
	}
	if len(guard.attempts[source]) != 1 {
		t.Fatal("challenge request skipped attempts")
	}
	if err := blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	policy.DailyLimit = 0
	_, err = accounts.ResolveClientLogin(context.Background(), creationPowUUID, source, CreationProof{})
	requireCreationRejection(t, err, CreationDisabled)
	policy.DailyLimit = 1
	if _, err := db.Exec(`INSERT INTO cn_account_creation_daily VALUES('2026-11-04',?,1)`, source.String()); err != nil {
		t.Fatal(err)
	}
	_, err = accounts.ResolveClientLogin(context.Background(), creationPowUUID, source, CreationProof{})
	requireCreationRejection(t, err, CreationDailyLimit)
	bad := CreationProof{Challenge: strings.Repeat("x", 100), Solution: "ffffffffffffffff"}
	if _, err := accounts.ResolveClientLogin(context.Background(), creationPrimaryUUID, source, bad); err != nil {
		t.Fatal("known identity required proof", err)
	}
	if _, err := accounts.BindClientAccount(context.Background(), creationPrimaryUUID, "known_pow", "valid-password", source, bad); err != nil {
		t.Fatal("known binding required proof", err)
	}
	if _, err := accounts.ResolveClientLogin(context.Background(), creationPrimaryUUID, source, CreationProof{Challenge: strings.Repeat("x", 101)}); err == nil {
		t.Fatal("known identity bypassed input shape")
	}
}

func TestCreationPowFinalPolicyAndExpiry(t *testing.T) {
	for _, test := range []struct {
		name           string
		initial, final int
		proof          int
		expire         bool
		challenge      int
	}{
		{"upgrade", 18, 20, 0, false, 20},
		{"downgrade", 20, 18, 1, false, 0},
		{"off", 20, 0, 1, true, 0},
		{"custom-upgrade", 18, 19, 0, false, 19},
		{"custom-downgrade", 20, 17, 1, false, 0},
		{"enable", 0, 18, 0, false, 18},
		{"expires", 18, 18, 0, true, 18},
	} {
		t.Run(test.name, func(t *testing.T) {
			accounts := creationTestAccounts(t)
			reads := 0
			guard := attachPowPolicy(t, accounts, func() CreationPolicy {
				reads++
				mode := test.initial
				if reads >= 3 {
					mode = test.final
				}
				return CreationPolicy{DailyLimit: 1, VerificationBits: mode}
			})
			guard.now = func() time.Time {
				if test.expire && reads >= 2 {
					return time.Unix(creationPowNow+120, 0)
				}
				return time.Unix(creationPowNow, 0)
			}
			_, err := accounts.ResolveClientLogin(context.Background(), creationPowUUID, netip.MustParseAddr("192.0.2.82"), fixedCreationProof(test.proof))
			if test.challenge != 0 {
				requireCreationChallenge(t, err, test.challenge)
				if creationCount(t, accounts) != 0 {
					t.Fatal("final proof failure consumed quota")
				}
				if binding, _ := accounts.AccountBinding(creationPowUUID); binding.UserID != 0 {
					t.Fatal("final proof failure retained identity")
				}
			} else if err != nil || creationCount(t, accounts) != 1 {
				t.Fatal("permitted final policy failed", err)
			}
		})
	}
}

func TestCreationPowSameUUIDConcurrencyRestartAndBindingRollback(t *testing.T) {
	accounts := creationTestAccounts(t)
	guard := attachPowPolicy(t, accounts, func() CreationPolicy { return CreationPolicy{DailyLimit: 1, VerificationBits: 18} })
	source := netip.MustParseAddr("192.0.2.83")
	// A credential conflict rolls back identity and daily quota; proof stays usable.
	if _, err := accounts.BindInternalAccount(creationPrimaryUUID, "taken_pow", "valid-password"); err != nil {
		t.Fatal(err)
	}
	_, err := accounts.BindClientAccount(context.Background(), creationPowUUID, "taken_pow", "valid-password", source, fixedCreationProof(0))
	var challenge *CreationChallengeRequired
	if err == nil || errors.As(err, &challenge) || creationCount(t, accounts) != 0 {
		t.Fatal("binding conflict did not roll back", err)
	}
	results := make(chan error, 4)
	accounts.mu.Lock()
	for index := range 4 {
		go func() {
			var err error
			if index == 0 {
				_, err = accounts.BindClientAccount(context.Background(), creationPowUUID, "bound_pow", "valid-password", source, fixedCreationProof(0))
			} else {
				_, err = accounts.ResolveClientLogin(context.Background(), creationPowUUID, source, fixedCreationProof(0))
			}
			results <- err
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		guard.mu.Lock()
		count := len(guard.attempts[source])
		guard.mu.Unlock()
		if count == 5 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	accounts.mu.Unlock()
	for range 4 {
		if err := <-results; err != nil {
			t.Fatal("same-UUID proof retry failed", err)
		}
	}
	if creationCount(t, accounts) != 1 {
		t.Fatal("same UUID was charged more than once")
	}
	if err := accounts.storage.Close(); err != nil {
		t.Fatal(err)
	}
	storage, err := OpenDatabase(accounts.storage.jsonPath, accounts.storage.seedPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	restarted, err := NewAccounts(storage)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.AttachCreationPolicy(func() CreationPolicy { return CreationPolicy{DailyLimit: 10, VerificationBits: 18} }); err != nil {
		t.Fatal(err)
	}
	restarted.creation.Load().now = guard.now
	if _, err := restarted.ResolveClientLogin(context.Background(), creationPowUUID, source, CreationProof{}); err != nil {
		t.Fatal("known identity challenged after restart", err)
	}
	// Old process signatures are rejected even while their token time is valid.
	proof := fixedCreationProof(0)
	if restarted.creation.Load().verifyProof(creationPowUUID, proof, guard.now()) != nil {
		t.Fatal("old process signing key survived restart")
	}
	proof.Challenge = strings.Repeat("A", 100)
	_, err = restarted.ResolveClientLogin(context.Background(), "01234567-89ab-4cde-8fab-0123456789ac", source, proof)
	requireCreationChallenge(t, err, 18)
}

func TestCreationPowCancellationAndFinalQuotaPriority(t *testing.T) {
	for _, quotaFull := range []bool{false, true} {
		accounts := creationTestAccounts(t)
		ctx, cancel := context.WithCancel(context.Background())
		reads := 0
		attachPowPolicy(t, accounts, func() CreationPolicy {
			reads++
			if reads >= 3 && !quotaFull {
				cancel()
			}
			mode := 18
			limit := 1
			if reads >= 3 && quotaFull {
				mode = 20
				limit = 0
			}
			return CreationPolicy{DailyLimit: limit, VerificationBits: mode}
		})
		_, err := accounts.BindClientAccount(ctx, creationPowUUID, "cancelled_pow", "valid-password", netip.MustParseAddr("192.0.2.84"), fixedCreationProof(0))
		cancel()
		if quotaFull {
			requireCreationRejection(t, err, CreationDisabled)
		} else if !errors.Is(err, context.Canceled) {
			t.Fatal("transaction cancellation lost", err)
		}
		if creationCount(t, accounts) != 0 {
			t.Fatal("cancelled or disabled final claim consumed quota")
		}
	}
}

func BenchmarkCreationPowVerification(b *testing.B) {
	guard := fixedCreationPowGuard()
	for _, test := range []struct {
		name  string
		proof CreationProof
	}{
		{"missing", CreationProof{}}, {"bad-mac", CreationProof{Challenge: strings.Repeat("A", 100), Solution: "0000000000000000"}},
		{"wrong-answer", CreationProof{Challenge: creationPowVectors[0].challenge, Solution: "0000000000000000"}},
		{"valid18", fixedCreationProof(0)}, {"valid20", fixedCreationProof(1)},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				guard.verifyProof(creationPowUUID, test.proof, guard.now())
			}
		})
	}
}
