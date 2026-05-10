package worker_test

import "encoding/json"

// jsonMarshal is a tiny test helper so the assertions can stay readable.
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
