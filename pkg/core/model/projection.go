package model

// ProjectData returns a new map containing only the specified fields from data.
// If fields is empty or nil, data is returned unmodified. Fields that do not
// exist in data are silently omitted.
func ProjectData(data map[string]any, fields []string) map[string]any {
	if len(fields) == 0 {
		return data
	}
	if data == nil {
		return nil
	}
	projected := make(map[string]any, len(fields))
	for _, f := range fields {
		if v, ok := data[f]; ok {
			projected[f] = v
		}
	}
	return projected
}
