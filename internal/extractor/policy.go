package extractor

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Local process policy allows a trusted operator to increase/decrease limits
// without treating a compressed archive size or manifest-supplied value as an
// expansion budget. Invalid overrides are errors, not changes to infinity.
func ConfiguredBudget() (Budget, error) {
	b := DefaultBudget()
	for _, kv := range []struct {
		name string
		dst  *int64
	}{{"XDEV_EXTRACT_MAX_FILE_BYTES", &b.MaxFileBytes}, {"XDEV_EXTRACT_MAX_TOTAL_BYTES", &b.MaxTotalBytes}} {
		if text, ok := os.LookupEnv(kv.name); ok {
			n, e := strconv.ParseInt(text, 10, 64)
			if e != nil || n <= 0 {
				return b, fmt.Errorf("invalid %s", kv.name)
			}
			*kv.dst = n
		}
	}
	for _, kv := range []struct {
		name string
		dst  *int
	}{{"XDEV_EXTRACT_MAX_ENTRIES", &b.MaxEntries}, {"XDEV_EXTRACT_MAX_DEPTH", &b.MaxDepth}} {
		if text, ok := os.LookupEnv(kv.name); ok {
			n, e := strconv.Atoi(text)
			if e != nil || n <= 0 {
				return b, fmt.Errorf("invalid %s", kv.name)
			}
			*kv.dst = n
		}
	}
	if text, ok := os.LookupEnv("XDEV_EXTRACT_TIMEOUT"); ok {
		d, e := time.ParseDuration(text)
		if e != nil || d <= 0 {
			return b, fmt.Errorf("invalid XDEV_EXTRACT_TIMEOUT")
		}
		b.Timeout = d
	}
	if text, ok := os.LookupEnv("XDEV_EXTRACT_RESERVE_BYTES"); ok {
		n, e := strconv.ParseUint(text, 10, 64)
		if e != nil {
			return b, fmt.Errorf("invalid XDEV_EXTRACT_RESERVE_BYTES")
		}
		b.ReserveBytes = n
	}
	return b, nil
}
