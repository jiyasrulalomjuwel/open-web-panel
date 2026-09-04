package validator

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
)

type Rule func(value interface{}) *errors.FieldError

func Required() Rule {
	return func(value interface{}) *errors.FieldError {
		if value == nil {
			return &errors.FieldError{Reason: "is required"}
		}
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) == "" {
				return &errors.FieldError{Reason: "must not be empty"}
			}
		case int, int64, float64:
			// numbers are never "empty"
		case []interface{}:
			if len(v) == 0 {
				return &errors.FieldError{Reason: "must not be empty"}
			}
		default:
			return &errors.FieldError{Reason: "is required"}
		}
		return nil
	}
}

func MinLength(min int) Rule {
	return func(value interface{}) *errors.FieldError {
		s, ok := value.(string)
		if !ok {
			return nil
		}
		if len(s) < min {
			return &errors.FieldError{Reason: fmt.Sprintf("must be at least %d characters", min)}
		}
		return nil
	}
}

func MaxLength(max int) Rule {
	return func(value interface{}) *errors.FieldError {
		s, ok := value.(string)
		if !ok {
			return nil
		}
		if len(s) > max {
			return &errors.FieldError{Reason: fmt.Sprintf("must not exceed %d characters", max)}
		}
		return nil
	}
}

func Min(minVal float64) Rule {
	return func(value interface{}) *errors.FieldError {
		switch v := value.(type) {
		case float64:
			if v < minVal {
				return &errors.FieldError{Reason: fmt.Sprintf("must be at least %v", minVal)}
			}
		case int:
			if float64(v) < minVal {
				return &errors.FieldError{Reason: fmt.Sprintf("must be at least %v", minVal)}
			}
		case string:
			f, err := strconv.ParseFloat(v, 64)
			if err == nil && f < minVal {
				return &errors.FieldError{Reason: fmt.Sprintf("must be at least %v", minVal)}
			}
		}
		return nil
	}
}

func Max(maxVal float64) Rule {
	return func(value interface{}) *errors.FieldError {
		switch v := value.(type) {
		case float64:
			if v > maxVal {
				return &errors.FieldError{Reason: fmt.Sprintf("must not exceed %v", maxVal)}
			}
		case int:
			if float64(v) > maxVal {
				return &errors.FieldError{Reason: fmt.Sprintf("must not exceed %v", maxVal)}
			}
		case string:
			f, err := strconv.ParseFloat(v, 64)
			if err == nil && f > maxVal {
				return &errors.FieldError{Reason: fmt.Sprintf("must not exceed %v", maxVal)}
			}
		}
		return nil
	}
}

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

func Email() Rule {
	return func(value interface{}) *errors.FieldError {
		s, ok := value.(string)
		if !ok {
			return &errors.FieldError{Reason: "must be a valid email address"}
		}
		if !emailRegex.MatchString(s) {
			return &errors.FieldError{Reason: "must be a valid email address"}
		}
		return nil
	}
}

var domainRegex = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?)*\.[a-z]{2,}$`)

func Domain() Rule {
	return func(value interface{}) *errors.FieldError {
		s, ok := value.(string)
		if !ok {
			return &errors.FieldError{Reason: "must be a valid domain name"}
		}
		if !domainRegex.MatchString(s) {
			return &errors.FieldError{Reason: "must be a valid domain name (e.g., example.com)"}
		}
		if len(s) > 253 {
			return &errors.FieldError{Reason: "domain name must not exceed 253 characters"}
		}
		return nil
	}
}

func Username() Rule {
	return func(value interface{}) *errors.FieldError {
		s, ok := value.(string)
		if !ok {
			return &errors.FieldError{Reason: "must be a valid username"}
		}
		if len(s) < 3 {
			return &errors.FieldError{Reason: "username must be at least 3 characters"}
		}
		if len(s) > 32 {
			return &errors.FieldError{Reason: "username must not exceed 32 characters"}
		}
		matched, _ := regexp.MatchString(`^[a-z][a-z0-9_\-]+$`, s)
		if !matched {
			return &errors.FieldError{Reason: "username must start with a letter and contain only lowercase letters, numbers, hyphens, and underscores"}
		}
		return nil
	}
}

var passwordRegex = regexp.MustCompile(`^.{8,128}$`)

func Password() Rule {
	return func(value interface{}) *errors.FieldError {
		s, ok := value.(string)
		if !ok {
			return &errors.FieldError{Reason: "must be a valid password"}
		}
		if !passwordRegex.MatchString(s) {
			return &errors.FieldError{Reason: "password must be between 8 and 128 characters"}
		}
		hasUpper, _ := regexp.MatchString(`[A-Z]`, s)
		hasLower, _ := regexp.MatchString(`[a-z]`, s)
		hasDigit, _ := regexp.MatchString(`[0-9]`, s)
		hasSpecial, _ := regexp.MatchString(`[!@#$%^&*(),.?":{}|<>_\-]`, s)
		if !hasUpper && !hasSpecial {
			return &errors.FieldError{Reason: "password must include at least one uppercase letter or special character"}
		}
		if !hasDigit && !hasSpecial {
			return &errors.FieldError{Reason: "password must include at least one digit or special character"}
		}
		if !hasLower {
			return &errors.FieldError{Reason: "password must include at least one lowercase letter"}
		}
		return nil
	}
}

func InList(allowed ...string) Rule {
	return func(value interface{}) *errors.FieldError {
		s, ok := value.(string)
		if !ok {
			return &errors.FieldError{Reason: fmt.Sprintf("must be one of: %s", strings.Join(allowed, ", "))}
		}
		for _, a := range allowed {
			if strings.EqualFold(s, a) {
				return nil
			}
		}
		return &errors.FieldError{Reason: fmt.Sprintf("must be one of: %s", strings.Join(allowed, ", "))}
	}
}

func Regex(pattern string, message string) Rule {
	re := regexp.MustCompile(pattern)
	return func(value interface{}) *errors.FieldError {
		s, ok := value.(string)
		if !ok {
			return &errors.FieldError{Reason: message}
		}
		if !re.MatchString(s) {
			return &errors.FieldError{Reason: message}
		}
		return nil
	}
}

func Port() Rule {
	return func(value interface{}) *errors.FieldError {
		switch v := value.(type) {
		case float64:
			if v < 1 || v > 65535 {
				return &errors.FieldError{Reason: "must be between 1 and 65535"}
			}
		case int:
			if v < 1 || v > 65535 {
				return &errors.FieldError{Reason: "must be between 1 and 65535"}
			}
		case string:
			p, err := strconv.Atoi(v)
			if err != nil || p < 1 || p > 65535 {
				return &errors.FieldError{Reason: "must be a valid port number (1-65535)"}
			}
		}
		return nil
	}
}

func IP() Rule {
	return func(value interface{}) *errors.FieldError {
		s, ok := value.(string)
		if !ok {
			return &errors.FieldError{Reason: "must be a valid IP address"}
		}
		if net.ParseIP(s) == nil {
			return &errors.FieldError{Reason: "must be a valid IP address"}
		}
		return nil
	}
}

func Integer() Rule {
	return func(value interface{}) *errors.FieldError {
		switch value.(type) {
		case int, int64, float64:
			return nil
		case string:
			_, err := strconv.Atoi(value.(string))
			if err != nil {
				return &errors.FieldError{Reason: "must be an integer"}
			}
			return nil
		default:
			return &errors.FieldError{Reason: "must be an integer"}
		}
	}
}

func Boolean() Rule {
	return func(value interface{}) *errors.FieldError {
		switch v := value.(type) {
		case bool:
			return nil
		case int:
			if v == 0 || v == 1 {
				return nil
			}
		case float64:
			if v == 0 || v == 1 {
				return nil
			}
		case string:
			switch strings.ToLower(v) {
			case "true", "false", "1", "0", "yes", "no":
				return nil
			}
		}
		return &errors.FieldError{Reason: "must be a boolean value"}
	}
}

type ValidationSpec struct {
	Rules map[string][]Rule
}

func New() *ValidationSpec {
	return &ValidationSpec{Rules: make(map[string][]Rule)}
}

func (vs *ValidationSpec) Add(field string, rules ...Rule) *ValidationSpec {
	vs.Rules[field] = append(vs.Rules[field], rules...)
	return vs
}

func (vs *ValidationSpec) Validate(data map[string]interface{}) *errors.AppError {
	var fieldErrors []errors.FieldError

	for field, rules := range vs.Rules {
		value, exists := data[field]

		for _, rule := range rules {
			if !exists {
				value = nil
			}
			if fe := rule(value); fe != nil {
				fe.Field = field
				fieldErrors = append(fieldErrors, *fe)
				break
			}
		}
	}

	if len(fieldErrors) > 0 {
		return errors.Validation("one or more fields are invalid", fieldErrors...)
	}
	return nil
}

var (
	validMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	validDomainTypes = []string{"primary", "addon", "parked", "subdomain"}
	validAccountStatuses = []string{"active", "suspended", "pending", "terminated"}
	validCMSPlatforms = []string{"wordpress", "joomla", "drupal", "prestashop", "magento"}
	validTicketStatuses = []string{"open", "closed", "replied", "pending"}
	validBackupTypes = []string{"full", "home", "database"}
	validDNSRecordTypes = []string{"A", "AAAA", "CNAME", "MX", "TXT", "SRV", "NS", "SOA"}
)

func ValidateDomainType(t string) *errors.FieldError {
	return InList(validDomainTypes...)(t)
}

func ValidateAccountStatus(s string) *errors.FieldError {
	return InList(validAccountStatuses...)(s)
}

func ValidateCMSPlatform(p string) *errors.FieldError {
	return InList(validCMSPlatforms...)(p)
}

func ValidateTicketStatus(s string) *errors.FieldError {
	return InList(validTicketStatuses...)(s)
}

func ValidateBackupType(t string) *errors.FieldError {
	return InList(validBackupTypes...)(t)
}

func ValidateDNSRecordType(t string) *errors.FieldError {
	return InList(validDNSRecordTypes...)(t)
}

func SanitizeDomain(domain string) string {
	d := strings.ReplaceAll(domain, "..", "")
	d = strings.ReplaceAll(d, "/", "")
	d = strings.ReplaceAll(d, "\\", "")
	return strings.TrimSpace(d)
}
