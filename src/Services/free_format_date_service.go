package Services

import (
	"cuento-backend/src/Entities"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func FreeFormatDateGenerateSortNumber(values map[string]interface{}, placeholders []Entities.FreeFormatDatePlaceholder) int64 {
	sorted := make([]Entities.FreeFormatDatePlaceholder, len(placeholders))
	copy(sorted, placeholders)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Position < sorted[j].Position
	})

	var result strings.Builder

	for _, p := range sorted {
		raw := values[p.Name]

		var component string

		switch p.Type {
		case Entities.FreeFormatDatePlaceholderTypeList:
			width := len(strconv.Itoa(len(p.ValueList)))
			idx := 0
			if raw != nil {
				strVal := fmt.Sprintf("%v", raw)
				for i, v := range p.ValueList {
					if v == strVal {
						idx = i + 1 // 1-based
						break
					}
				}
			}
			component = fmt.Sprintf("%0*d", width, idx)

		case Entities.FreeFormatDatePlaceholderTypeNumber:
			hasNegativeMin := p.MinValue != nil && *p.MinValue < 0

			var normalizedMax int64
			if p.MaxValue != nil {
				normalizedMax = int64(*p.MaxValue)
				if hasNegativeMin {
					normalizedMax -= int64(*p.MinValue)
				}
			}
			width := len(strconv.FormatInt(normalizedMax, 10))

			if raw == nil {
				component = fmt.Sprintf("%0*d", width, 0)
			} else {
				var numVal int64
				switch v := raw.(type) {
				case float64:
					numVal = int64(v)
				case int:
					numVal = int64(v)
				case int64:
					numVal = v
				case string:
					numVal, _ = strconv.ParseInt(v, 10, 64)
				}

				normalizedVal := numVal
				if hasNegativeMin {
					normalizedVal = numVal - int64(*p.MinValue)
				}
				component = fmt.Sprintf("%0*d", width, normalizedVal)
			}
		}

		result.WriteString(component)
	}

	val, _ := strconv.ParseInt(result.String(), 10, 64)
	return val
}
