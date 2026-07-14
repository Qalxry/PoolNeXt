package security

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestConsumeCodeWithKeyIsSingleUseUnderConcurrency(t *testing.T) {
	resetVerificationMapForTest()
	RegisterVerificationCodeWithKey("user@example.com", "123456", EmailVerificationPurpose)

	var successes atomic.Int32
	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			if ConsumeCodeWithKey("user@example.com", "123456", EmailVerificationPurpose) {
				successes.Add(1)
			}
		}()
	}
	group.Wait()

	if got := successes.Load(); got != 1 {
		t.Fatalf("verification code was consumed %d times, want exactly once", got)
	}
}

func TestConsumeCodeWithKeyKeepsCodeAfterWrongAttempt(t *testing.T) {
	resetVerificationMapForTest()
	RegisterVerificationCodeWithKey("user@example.com", "123456", EmailVerificationPurpose)

	if ConsumeCodeWithKey("user@example.com", "000000", EmailVerificationPurpose) {
		t.Fatal("wrong verification code was accepted")
	}
	if !ConsumeCodeWithKey("user@example.com", "123456", EmailVerificationPurpose) {
		t.Fatal("wrong attempt consumed the valid verification code")
	}
}

func resetVerificationMapForTest() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
