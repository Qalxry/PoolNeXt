package security

import (
	"crypto/subtle"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type verificationValue struct {
	code string
	time time.Time
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
)

var verificationMutex sync.Mutex
var verificationMap map[string]verificationValue
var verificationMapMaxSize = 10
var VerificationValidMinutes = 10

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	return code[:length]
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap[purpose+key] = verificationValue{
		code: code,
		time: time.Now(),
	}
	if len(verificationMap) > verificationMapMaxSize {
		removeExpiredPairs()
	}
}

func VerifyCodeWithKey(key string, code string, purpose string) bool {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	return verifyCodeWithKeyLocked(key, code, purpose, false)

}

// ConsumeCodeWithKey verifies and deletes a code while holding the same lock,
// so concurrent reset or binding requests cannot reuse one credential.
func ConsumeCodeWithKey(key string, code string, purpose string) bool {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	return verifyCodeWithKeyLocked(key, code, purpose, true)
}

func verifyCodeWithKeyLocked(key string, code string, purpose string, consume bool) bool {
	mapKey := purpose + key
	value, okay := verificationMap[mapKey]
	if !okay {
		return false
	}
	if time.Since(value.time) >= time.Duration(VerificationValidMinutes)*time.Minute {
		delete(verificationMap, mapKey)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(value.code)) != 1 {
		return false
	}
	if consume {
		delete(verificationMap, mapKey)
	}
	return true
}

func DeleteKey(key string, purpose string) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, purpose+key)
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeExpiredPairs() {
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
