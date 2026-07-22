package validation

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/vigilagent/vigilagent/pkg/response"
)

var (
	emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	uuidRegex  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// Errors collection
type Errors []response.ValidationErrorDetail

// Add a validation error
func (e *Errors) Add(field, rule, message string) {
	*e = append(*e, response.ValidationErrorDetail{
		Field:   field,
		Rule:    rule,
		Message: message,
	})
}

// Validator validates inputs
type Validator struct {
	Errors Errors
}

// New creates a new validator instance
func New() *Validator {
	return &Validator{
		Errors: make(Errors, 0),
	}
}

// Required checks if a string is empty
func (v *Validator) Required(field, val string) *Validator {
	if strings.TrimSpace(val) == "" {
		v.Errors.Add(field, "required", field+" is required")
	}
	return v
}

// MinLength checks if string length is below minimum
func (v *Validator) MinLength(field, val string, min int) *Validator {
	if len(val) < min {
		v.Errors.Add(field, "min_length", field+" must be at least "+strconvItoa(min)+" characters")
	}
	return v
}

// Email checks email format
func (v *Validator) Email(field, val string) *Validator {
	if val != "" && !emailRegex.MatchString(val) {
		v.Errors.Add(field, "email", field+" must be a valid email address")
	}
	return v
}

// UUID checks UUID format
func (v *Validator) UUID(field, val string) *Validator {
	if val != "" && !uuidRegex.MatchString(val) {
		v.Errors.Add(field, "uuid", field+" must be a valid UUID")
	}
	return v
}

// HasErrors returns true if any errors exist
func (v *Validator) HasErrors() bool {
	return len(v.Errors) > 0
}

// WriteResponse writes standard validation response if errors exist.
// Returns true if there were errors (so the handler can return early).
func (v *Validator) WriteResponse(w http.ResponseWriter, r *http.Request) bool {
	if v.HasErrors() {
		response.ValidationErrorResponse(w, r, v.Errors)
		return true
	}
	return false
}

// DecodeAndValidate decodes json into body and validates it
func DecodeAndValidate(w http.ResponseWriter, r *http.Request, body interface{}) (*Validator, bool) {
	if err := json.NewDecoder(r.Body).Decode(body); err != nil {
		response.BadRequestR(w, r, "invalid request body")
		return nil, false
	}
	return New(), true
}

func strconvItoa(n int) string {
	return string(strconvAppendInt(nil, int64(n)))
}

func strconvAppendInt(dst []byte, n int64) []byte {
	return strconvAppendIntBase(dst, n, 10)
}

func strconvAppendIntBase(dst []byte, n int64, base int) []byte {
	if base < 2 || base > 36 {
		panic("invalid base")
	}
	var buf [64]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	if n == 0 {
		i--
		buf[i] = '0'
	} else {
		for n > 0 {
			i--
			d := n % int64(base)
			if d < 10 {
				buf[i] = byte('0' + d)
			} else {
				buf[i] = byte('a' + d - 10)
			}
			n /= int64(base)
		}
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return append(dst, buf[i:]...)
}
