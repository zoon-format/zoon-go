package zoon

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

func (e *Encoder) encode(v any) error {
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Slice, reflect.Array:
		return e.encodeTabular(val)
	case reflect.Struct, reflect.Map:
		return e.encodeInline(val)
	default:
		return fmt.Errorf("%w: top level must be object or array", ErrInvalidFormat)
	}
}

func flattenValue(prefix string, v reflect.Value, result map[string]any) {
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Ptr {
		if v.IsNil() {
			result[prefix] = nil
			return
		}
		v = v.Elem()
	}

	if v.Kind() == reflect.Map {
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		for _, k := range keys {
			newKey := k.String()
			if prefix != "" {
				newKey = prefix + "." + newKey
			}
			flattenValue(newKey, v.MapIndex(k), result)
		}
	} else if v.Kind() == reflect.Struct {
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := f.Name
			tag := f.Tag.Get("zoon")
			if tag == "" {
				tag = f.Tag.Get("json")
			}
			if tag == "-" {
				continue
			}
			if parts := strings.Split(tag, ","); parts[0] != "" {
				name = parts[0]
			}

			newKey := name
			if prefix != "" {
				newKey = prefix + "." + newKey
			}
			flattenValue(newKey, v.Field(i), result)
		}
	} else {
		// Primitive or array (arrays treated as values in tabular for now unless we recursive flatten list items?)
		// Spec implies arrays are values like [1,2] in tabular.
		// So we stop recursion here.
		if v.IsValid() {
			result[prefix] = v.Interface()
		} else {
			result[prefix] = nil
		}
	}
}

type columnStats struct {
	name       string
	kind       reflect.Kind
	values     []string
	uniqueVals map[string]bool
	isSeq      bool
	indexed    bool
	enumKeys   []string
	isText     bool
}

func detectAliases(keys []string) map[string]string {
	prefixCounts := make(map[string]int)
	for _, key := range keys {
		parts := strings.Split(key, ".")
		if len(parts) > 1 {
			for i := 1; i < len(parts); i++ {
				prefix := strings.Join(parts[:i], ".")
				prefixCounts[prefix]++
			}
		}
	}

	type saving struct {
		prefix string
		score  int
	}
	var savings []saving

	for prefix, count := range prefixCounts {
		prefixLen := len(prefix)
		// Savings: (len - 2) * count - (len + 4)
		score := (prefixLen-2)*count - (prefixLen + 4)
		if score > 0 {
			savings = append(savings, saving{prefix, score})
		}
	}

	sort.Slice(savings, func(i, j int) bool { return savings[i].score > savings[j].score })

	aliases := make(map[string]string)
	usedAliases := make(map[string]bool)
	aliasIdx := 0

	for _, s := range savings {
		// Simplified: assign aliases roughly
		// Ideally ensure we don't alias sub-parts if parent is aliased, or handle nested aliases.
		// For now simple single-level check or just greedy.

		pParts := strings.Split(s.prefix, ".")
		candidate := strings.ToLower(string(pParts[len(pParts)-1][0])) // First char of last part

		validAlias := ""
		for {
			if !usedAliases[candidate] {
				validAlias = candidate
				break
			}
			if len(candidate) == 1 {
				// Try a, b, c...
				candidate = string(rune('a' + aliasIdx))
				aliasIdx++
				if aliasIdx > 25 {
					break
				}
			} else {
				break
			}
		}

		if validAlias != "" {
			aliases[s.prefix] = validAlias
			usedAliases[validAlias] = true
		}
		if len(aliases) >= 10 {
			break
		}
	}
	return aliases
}

func applyAlias(name string, aliases map[string]string) string {
	for prefix, alias := range aliases {
		if strings.HasPrefix(name, prefix+".") {
			return "%" + alias + name[len(prefix):]
		}
		if name == prefix {
			return "%" + alias
		}
	}
	return name
}

func (e *Encoder) encodeTabular(slice reflect.Value) error {
	length := slice.Len()
	if length == 0 {
		return nil
	}

	// 1. Flatten Data
	var flattened []map[string]any
	keySet := make(map[string]bool)

	for i := 0; i < length; i++ {
		item := slice.Index(i)
		rowMap := make(map[string]any)
		flattenValue("", item, rowMap)
		flattened = append(flattened, rowMap)
		for k := range rowMap {
			keySet[k] = true
		}
	}

	var allKeys []string
	for k := range keySet {
		allKeys = append(allKeys, k)
	}
	sort.Strings(allKeys)

	// 2. Identify Constants
	constants := make(map[string]any)
	var activeKeys []string

	if length > 1 {
		for _, k := range allKeys {
			isConst := true
			first := flattened[0][k]
			for _, row := range flattened {
				val := row[k]
				if val != first && (val != nil || first != nil) { // Go equality check
					// Deep check for maps/slices?
					if !reflect.DeepEqual(val, first) {
						isConst = false
						break
					}
				}
			}
			if isConst && first != nil {
				constants[k] = first
			} else {
				activeKeys = append(activeKeys, k)
			}
		}
	} else {
		activeKeys = allKeys
	}

	// 3. Stats & Types for Active Keys
	stats := make(map[string]*columnStats)
	for _, k := range activeKeys {
		s := &columnStats{
			name:       k,
			uniqueVals: make(map[string]bool),
		}
		for _, row := range flattened {
			v := row[k]

			// Detect kind logic
			valRef := reflect.ValueOf(v)
			if !valRef.IsValid() || (valRef.Kind() == reflect.Ptr && valRef.IsNil()) {
				// sVal = "~" handles later
			} else {
				if s.kind == reflect.Invalid {
					s.kind = valRef.Kind()
				} else if s.kind != valRef.Kind() {
					s.kind = reflect.String // mixed types fallback
				}
			}

			// Value for sequencing
			if k == "id" && canBeInt(s.kind) {
				s.isSeq = true // candidate
			}
		}
		stats[k] = s
	}

	// Collect String Values for active keys
	for _, row := range flattened {
		for _, k := range activeKeys {
			v := row[k]
			sVal := serializeValue(reflect.ValueOf(v))
			stats[k].values = append(stats[k].values, sVal)
			stats[k].uniqueVals[sVal] = true
		}
	}

	// 4. Aliases
	aliases := detectAliases(activeKeys)

	// 5. Build Header
	var lines []string

	// Alias Defs
	if len(aliases) > 0 {
		var parts []string
		for prefix, alias := range aliases {
			parts = append(parts, fmt.Sprintf("%%%s=%s", alias, prefix))
		}
		sort.Slice(parts, func(i, j int) bool { return parts[i] < parts[j] }) // deterministic
		lines = append(lines, strings.Join(parts, " "))
	}

	var headerParts []string
	headerParts = append(headerParts, "#")

	// Constants
	// Need sorted keys for deterministic output
	var constKeys []string
	for k := range constants {
		constKeys = append(constKeys, k)
	}
	sort.Strings(constKeys)

	for _, k := range constKeys {
		val := constants[k]
		aliased := applyAlias(k, aliases)
		aliased = strings.ReplaceAll(aliased, " ", "_")

		sVal := serializeValue(reflect.ValueOf(val))
		typeCode := ":" // inferred
		if _, ok := val.(string); ok {
			typeCode = "="
		}

		if typeCode == ":" {
			if b, ok := val.(bool); ok {
				if b {
					sVal = "y"
				} else {
					sVal = "n"
				}
			}
		}
		headerParts = append(headerParts, fmt.Sprintf("@%s%s%s", aliased, typeCode, sVal))
	}

	var skipIndices []int

	for i, k := range activeKeys {
		st := stats[k]
		typeCode := "s"

		aliased := applyAlias(k, aliases)
		aliased = strings.ReplaceAll(aliased, " ", "_")

		if st.isSeq {
			isSeq := true
			for idx, val := range st.values {
				if val != fmt.Sprintf("%d", idx+1) {
					isSeq = false
					break
				}
			}
			if isSeq {
				typeCode = "i+"
				skipIndices = append(skipIndices, i)
			} else {
				typeCode = "i"
			}
		} else if isBoolKind(st.kind) {
			typeCode = "b"
		} else if isIntKind(st.kind) {
			typeCode = "i"
		} else {
			if len(st.uniqueVals) <= 10 && len(st.uniqueVals) < length {
				var keys []string
				for k := range st.uniqueVals {
					if k != "~" {
						keys = append(keys, k)
					}
				}
				sort.Strings(keys)
				if len(keys) >= 3 {
					avgLen := 0
					for _, k := range keys {
						avgLen += len(k)
					}
					avgLen = avgLen / len(keys)
					literalCost := avgLen * length
					indexCost := len(strings.Join(keys, "|")) + length*2
					if literalCost > indexCost {
						typeCode = "!" + strings.Join(keys, "|")
						st.indexed = true
						st.enumKeys = keys
					} else {
						typeCode = "=" + strings.Join(keys, "|")
						st.enumKeys = keys
					}
				} else if len(keys) > 0 {
					typeCode = "=" + strings.Join(keys, "|")
					st.enumKeys = keys
				}
			} else {
				totalLen := 0
				for _, v := range st.values {
					totalLen += len(v)
				}
				if len(st.values) > 0 && totalLen/len(st.values) > 30 {
					typeCode = "t"
					st.isText = true
				}
			}
		}

		if strings.HasPrefix(typeCode, "=") || strings.HasPrefix(typeCode, "!") {
			headerParts = append(headerParts, aliased+typeCode)
		} else {
			headerParts = append(headerParts, fmt.Sprintf("%s:%s", aliased, typeCode))
		}
	}

	// +N optimization
	// Determine if all active columns are non-consuming (i.e., i+ or constant)
	// We rely on skipIndices which are populated for i+
	// Constants are stripped from activeKeys entirely.

	allSkipped := true
	if len(activeKeys) == 0 {
		// If no active keys (all implicit), then row is empty/skipped
	} else {
		for i := 0; i < len(activeKeys); i++ {
			skipped := false
			for _, s := range skipIndices {
				if i == s {
					skipped = true
					break
				}
			}
			if !skipped {
				allSkipped = false
				break
			}
		}
	}

	// fmt.Printf("DEBUG: active=%v skip=%v allSkipped=%v\n", activeKeys, skipIndices, allSkipped)

	if allSkipped && length > 0 {
		headerParts = append(headerParts, fmt.Sprintf("+%d", length))
	}

	lines = append(lines, strings.Join(headerParts, " "))

	headerBlock := strings.Join(lines, "\n")

	if allSkipped {
		fmt.Fprintf(e.w, "%s\n", headerBlock)
		return nil
	}

	fmt.Fprintf(e.w, "%s\n", headerBlock)

	for rIdx, row := range flattened {
		var outRow []string
		for i, k := range activeKeys {
			// Check skip
			skipped := false
			for _, s := range skipIndices {
				if i == s {
					skipped = true
					break
				}
			}
			if skipped {
				continue
			}

			// Stats for type check (bool conversion)
			// Actually we already serialized in stats, use that or re-serialize?
			// Re-serialize with bool logic

			// Warning: we need to respect the typeCode chosen.
			// If we chose 'b', we need 0/1. If 'i', number.

			rawVal := row[k]
			valRef := reflect.ValueOf(rawVal)
			sVal := serializeValue(valRef)

			if isBoolKind(stats[k].kind) {
				if sVal == "true" {
					sVal = "1"
				} else if sVal == "false" {
					sVal = "0"
				}
			} else if stats[k].indexed && len(stats[k].enumKeys) > 0 {
				for idx, enumVal := range stats[k].enumKeys {
					if sVal == enumVal {
						sVal = fmt.Sprintf("%d", idx)
						break
					}
				}
			} else if stats[k].isText {
				rawStr := ""
				if s, ok := rawVal.(string); ok {
					rawStr = s
				} else {
					rawStr = fmt.Sprintf("%v", rawVal)
				}
				sVal = `"` + strings.ReplaceAll(rawStr, `"`, `\"`) + `"`
			}
			outRow = append(outRow, sVal)
		}
		fmt.Fprintf(e.w, "%s\n", strings.Join(outRow, " "))
		_ = rIdx
	}

	return nil
}

func (e *Encoder) encodeInline(val reflect.Value) error {
	if val.Kind() == reflect.Interface || val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	var parts []string

	if val.Kind() == reflect.Map {
		keys := val.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		for _, k := range keys {
			v := val.MapIndex(k)
			parts = append(parts, formatInlinePair(k.String(), v))
		}
	} else if val.Kind() == reflect.Struct {
		t := val.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get("zoon")
			if tag == "" {
				tag = f.Tag.Get("json")
			}
			if tag == "-" {
				continue
			}
			name := f.Name
			if parts := strings.Split(tag, ","); parts[0] != "" {
				name = parts[0]
			}

			parts = append(parts, formatInlinePair(name, val.Field(i)))
		}
	}

	_, err := fmt.Fprintf(e.w, "%s", strings.Join(parts, " "))
	return err
}

func formatInlinePair(key string, v reflect.Value) string {
	valStr := serializeValue(v)
	if v.Kind() == reflect.String {
		valStr = strings.ReplaceAll(valStr, " ", "_")
		return fmt.Sprintf("%s=%s", key, valStr)
	}

	if v.Kind() == reflect.Bool {
		if v.Bool() {
			return fmt.Sprintf("%s:y", key)
		}
		return fmt.Sprintf("%s:n", key)
	}

	return fmt.Sprintf("%s:%s", key, valStr)
}

func serializeValue(v reflect.Value) string {
	if !v.IsValid() {
		return "~"
	}
	if v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return "~"
		}
		return serializeValue(v.Elem())
	}

	switch v.Kind() {
	case reflect.String:
		s := v.String()
		return strings.ReplaceAll(s, " ", "_")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", v.Int())
	case reflect.Bool:
		return fmt.Sprintf("%t", v.Bool())
	case reflect.Struct, reflect.Map:
		var buf strings.Builder
		enc := &Encoder{w: &buf}
		if err := enc.encodeInline(v); err != nil {
			return "{error}"
		}
		return "{" + buf.String() + "}"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func canBeInt(k reflect.Kind) bool {
	return k == reflect.Int || k == reflect.Int8 || k == reflect.Int16 || k == reflect.Int32 || k == reflect.Int64
}

func isIntKind(k reflect.Kind) bool {
	return canBeInt(k)
}

func isBoolKind(k reflect.Kind) bool {
	return k == reflect.Bool
}
