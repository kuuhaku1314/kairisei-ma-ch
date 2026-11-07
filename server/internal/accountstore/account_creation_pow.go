package accountstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	CreationVerificationRequired = "account_creation_verification_required"
	creationVerificationMessage  = "正在进行首次进入验证，请稍候…"
)

// The client solves one challenge for at most this long on a monotonic clock
// (CreationChallenge.SolveBudgetSeconds): 2 minutes up to 20 bits, doubling per
// extra bit so a given device keeps the same timeout odds, capped at 30 minutes.
func creationSolveBudget(difficulty byte) time.Duration {
	if difficulty <= 20 {
		return 2 * time.Minute
	}
	return min(2*time.Minute<<(difficulty-20), 30*time.Minute)
}

// Validity covers the whole client budget plus the two network legs.
func creationChallengeTTL(difficulty byte) time.Duration {
	return creationSolveBudget(difficulty) + 2*time.Minute
}

type CreationProof struct {
	Challenge string `json:"challenge"`
	Solution  string `json:"solution"`
}

// Missing proof is the zero value; an explicit malformed JSON value is input
// failure even for a known identity. Business validity is checked only for new ones.
func (proof *CreationProof) UnmarshalJSON(data []byte) error {
	type fields CreationProof
	var decoded fields
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) || json.Unmarshal(data, &decoded) != nil || len(decoded.Challenge) > 100 || len(decoded.Solution) > 16 {
		return errors.New("account creation proof has invalid shape")
	}
	*proof = CreationProof(decoded)
	return nil
}

type CreationChallenge struct {
	Algorithm     string `json:"algorithm"`
	Challenge     string `json:"challenge"`
	Difficulty    int    `json:"difficulty"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
}

type CreationChallengeRequired struct {
	Verification CreationChallenge
}

func (*CreationChallengeRequired) Error() string { return creationVerificationMessage }

// These values exist only within one request, after MAC and solution validation.
// They cannot be supplied as the public proof parameter.
type verifiedCreationProof struct {
	uuid       string
	difficulty byte
	expires    int64
}

type creationProofRequired struct{ difficulty byte }

func (*creationProofRequired) Error() string { return creationVerificationMessage }

func (admission *creationAdmission) precheckProof(policy CreationPolicy, now time.Time) error {
	difficulty, err := policy.difficulty()
	if err != nil {
		return err
	}
	if difficulty == 0 {
		return nil
	}
	admission.verified = admission.guard.verifyProof(admission.uuid, admission.proof, now)
	return admission.checkFinalProof(policy, now)
}

func (admission *creationAdmission) checkFinalProof(policy CreationPolicy, now time.Time) error {
	difficulty, err := policy.difficulty()
	if err != nil {
		return err
	}
	if difficulty == 0 {
		return nil
	}
	proof := admission.verified
	if proof == nil || proof.uuid != admission.uuid || proof.difficulty < difficulty || now.Unix() >= proof.expires {
		return &creationProofRequired{difficulty: difficulty}
	}
	return nil
}

// The private transaction/lock-owning use case has returned and rolled back
// before this conversion signs a new challenge, including final-policy changes.
func (admission *creationAdmission) finish(ctx context.Context, err error) error {
	var required *creationProofRequired
	if !errors.As(err, &required) {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	challenge, issueErr := admission.guard.issueChallenge(admission.uuid, required.difficulty)
	if issueErr != nil {
		return issueErr
	}
	return &CreationChallengeRequired{Verification: challenge}
}

func creationUUIDBytes(uuid string) ([16]byte, error) {
	var value [16]byte
	if !validLoginUUID(uuid) {
		return value, errors.New("invalid creation challenge UUID")
	}
	_, err := hex.Decode(value[:], []byte(strings.ReplaceAll(uuid, "-", "")))
	return value, err
}

func encodeCreationChallenge(key [32]byte, uuid [16]byte, difficulty byte, expires int64, nonce [16]byte) string {
	var token [75]byte
	token[0], token[1], token[2] = 1, 1, difficulty
	binary.BigEndian.PutUint64(token[3:11], uint64(expires))
	copy(token[11:27], uuid[:])
	copy(token[27:43], nonce[:])
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(token[:43])
	copy(token[43:], mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString(token[:])
}

func (guard *creationGuard) issueChallenge(uuid string, difficulty byte) (CreationChallenge, error) {
	identity, err := creationUUIDBytes(uuid)
	if err != nil {
		return CreationChallenge{}, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return CreationChallenge{}, fmt.Errorf("generate creation challenge nonce: %w", err)
	}
	expires := guard.now().Add(creationChallengeTTL(difficulty)).Unix()
	return CreationChallenge{Algorithm: "sha256-prefix-v1", Challenge: encodeCreationChallenge(guard.signingKey, identity, difficulty, expires, nonce), Difficulty: int(difficulty), ExpiresAtUnix: expires}, nil
}

func (guard *creationGuard) verifyProof(uuid string, proof CreationProof, now time.Time) *verifiedCreationProof {
	if len(proof.Challenge) != 100 || len(proof.Solution) != 16 {
		return nil
	}
	for _, c := range proof.Challenge {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return nil
		}
	}
	for _, c := range proof.Solution {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return nil
		}
	}
	token, err := base64.RawURLEncoding.Strict().DecodeString(proof.Challenge)
	if err != nil || len(token) != 75 || base64.RawURLEncoding.EncodeToString(token) != proof.Challenge {
		return nil
	}
	mac := hmac.New(sha256.New, guard.signingKey[:])
	_, _ = mac.Write(token[:43])
	if !hmac.Equal(token[43:], mac.Sum(nil)) {
		return nil
	}
	if token[0] != 1 || token[1] != 1 || token[2] < 16 || token[2] > 32 {
		return nil
	}
	identity, err := creationUUIDBytes(uuid)
	if err != nil || !bytes.Equal(identity[:], token[11:27]) {
		return nil
	}
	expires := int64(binary.BigEndian.Uint64(token[3:11]))
	if now.Unix() >= expires {
		return nil
	}
	digest := sha256.Sum256([]byte(proof.Challenge + ":" + proof.Solution))
	if !creationDigestMatches(digest, token[2]) {
		return nil
	}
	return &verifiedCreationProof{uuid: uuid, difficulty: token[2], expires: expires}
}

func creationDigestMatches(digest [32]byte, difficulty byte) bool {
	// v1 supports 16–32 zero leading bits, all within the first digest word.
	return difficulty >= 16 && difficulty <= 32 && binary.BigEndian.Uint32(digest[:4])>>(32-difficulty) == 0
}
