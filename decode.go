package zoon

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

func (d *Decoder) decode(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("zoon: Unmarshal(non-pointer %v)", rv.Type())
	}

	data, err := io.ReadAll(d.r)
	if err != nil {
		return err
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil
	}

	// If starts with % or #, it's tabular with potential aliases
	if data[0] == '#' || data[0] == '%' {
		return d.decodeTabular(data, rv)
	}
	return d.decodeInline(string(data), rv)
}

type headerField struct {
	name    string
	typ     string
	val     string
	indexed bool
	options []string
}

func (d *Decoder) decodeTabular(data []byte, rv reflect.Value) error {
	scanner := bufio.NewScanner(bytes.NewReader(data))

	aliases := make(map[string]string)
	var headerLine string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "%") {
			parts := strings.Fields(line)
			for _, p := range parts {
				if idx := strings.Index(p, "="); idx != -1 {
					alias := p[1:idx]
					prefix := p[idx+1:]
					if strings.HasPrefix(alias, "%") {
						alias = alias[1:]
					} // double check
					aliases[alias] = prefix
				}
			}
		} else if strings.HasPrefix(line, "#") {
			headerLine = line
			break
		} else {
			// Should not happen if compliant, but maybe direct data?
			// Assume implicit header not supported for now.
			return fmt.Errorf("zoon: invalid format, expected header")
		}
	}

	if headerLine == "" {
		return fmt.Errorf("zoon: missing header")
	}

	headerParts := strings.Fields(headerLine[2:])
	var headers []headerField
	var constants []headerField // using same struct for convenience
	explicitRows := -1

	for _, part := range headerParts {
		if strings.HasPrefix(part, "+") {
			if n, err := strconv.Atoi(part[1:]); err == nil {
				explicitRows = n
			}
			continue
		}

		isConst := false
		if strings.HasPrefix(part, "@") {
			isConst = true
			part = part[1:]
		}

		// Expand alias
		// Split name from type
		sepIdx := strings.IndexAny(part, ":=!")
		if sepIdx == -1 {
			continue
		}

		name := part[:sepIdx]
		typVal := part[sepIdx:] // includes separator

		// Unalias name
		if strings.HasPrefix(name, "%") {
			dotIdx := strings.Index(name, ".")
			if dotIdx != -1 {
				aName := name[1:dotIdx]
				suffix := name[dotIdx+1:]
				if prefix, ok := aliases[aName]; ok {
					name = prefix + "." + suffix
				}
			} else {
				if prefix, ok := aliases[name[1:]]; ok {
					name = prefix
				}
			}
		}

		sep := typVal[0]
		suffix := typVal[1:]

		hf := headerField{name: name}

		if isConst {
			hf.val = suffix
			if sep == '=' {
				hf.typ = "s"
				hf.val = strings.ReplaceAll(hf.val, "_", " ")
			} else {
				// :type or :value (inferred)
				// If suffix is a type code like 'i' or 'b', then it's not a value (?)
				// Actually constant syntax is @name=value or @name:value
				// If value is simple, it's inferred.
				hf.typ = "inferred"
			}
			constants = append(constants, hf)
		} else {
			if sep == '=' {
				hf.typ = "s"
				hf.options = strings.Split(suffix, "|")
			} else if sep == '!' {
				hf.typ = "s"
				hf.indexed = true
				hf.options = strings.Split(suffix, "|")
			} else {
				hf.typ = suffix
			}
			headers = append(headers, hf)
		}
	}

	sliceVal := rv.Elem()
	if sliceVal.Kind() == reflect.Slice {
		sliceVal.SetLen(0)
	} else if sliceVal.Kind() != reflect.Array {
		return fmt.Errorf("zoon: tabular format expects slice, got %v", sliceVal.Kind())
	}

	elemType := sliceVal.Type().Elem()
	isPtr := false
	if elemType.Kind() == reflect.Ptr {
		elemType = elemType.Elem()
		isPtr = true
	}

	autoIncID := 0

	processRow := func(vals []string) error {
		newElem := reflect.New(elemType).Elem()

		// Apply constants
		for _, c := range constants {
			valStr := c.val
			// Infer type logic if needed, setField handles basic types
			if err := setDeepField(newElem, c.name, "auto", valStr); err != nil {
				return err
			}
		}

		valIdx := 0
		for _, h := range headers {
			var valStr string

			if h.typ == "i+" {
				autoIncID++
				valStr = fmt.Sprintf("%d", autoIncID)
			} else {
				if valIdx >= len(vals) {
					// Missing value? Null?
					valStr = "~"
				} else {
					valStr = vals[valIdx]
					valIdx++
				}
			}

			if valStr == "~" {
				continue
			}

			if h.indexed && len(h.options) > 0 {
				if idx, err := strconv.Atoi(valStr); err == nil && idx >= 0 && idx < len(h.options) {
					valStr = h.options[idx]
				}
			}

			if err := setDeepField(newElem, h.name, h.typ, valStr); err != nil {
				return err
			}
		}

		if isPtr {
			newPtr := reflect.New(elemType)
			newPtr.Elem().Set(newElem)
			sliceVal.Set(reflect.Append(sliceVal, newPtr))
		} else {
			sliceVal.Set(reflect.Append(sliceVal, newElem))
		}
		return nil
	}

	if explicitRows > 0 {
		for i := 0; i < explicitRows; i++ {
			if err := processRow(nil); err != nil {
				return err
			}
		}
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		vals := tokenizeRow(line)
		if err := processRow(vals); err != nil {
			return err
		}
	}

	rv.Elem().Set(sliceVal)
	return nil
}

func (d *Decoder) decodeInline(data string, rv reflect.Value) error {
	target := rv.Elem()
	if target.Kind() == reflect.Ptr {
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		target = target.Elem()
	}

	parser := &inlineParser{input: data}
	pairs, err := parser.parse()
	if err != nil {
		return err
	}

	for _, p := range pairs {
		val := p.value
		if p.sep == "=" {
			val = strings.ReplaceAll(val, "_", " ")
		} else if strings.HasPrefix(val, "{") && strings.HasSuffix(val, "}") {

		}

		if err := setDeepField(target, p.key, "auto", val); err != nil {
			return err
		}
	}

	return nil
}

type inlinePair struct {
	key, sep, value string
}

type inlineParser struct {
	input string
	pos   int
}

func (p *inlineParser) parse() ([]inlinePair, error) {
	var pairs []inlinePair
	for p.pos < len(p.input) {
		p.skipSpace()
		if p.pos >= len(p.input) {
			break
		}

		keyStart := p.pos
		for p.pos < len(p.input) && p.input[p.pos] != ':' && p.input[p.pos] != '=' && p.input[p.pos] != ' ' {
			p.pos++
		}
		key := p.input[keyStart:p.pos]

		if p.pos >= len(p.input) {
			return nil, fmt.Errorf("unexpected end after key %s", key)
		}

		sep := string(p.input[p.pos])
		p.pos++

		valStart := p.pos
		if p.pos < len(p.input) && p.input[p.pos] == '{' {
			depth := 1
			p.pos++
			for p.pos < len(p.input) && depth > 0 {
				if p.input[p.pos] == '{' {
					depth++
				}
				if p.input[p.pos] == '}' {
					depth--
				}
				p.pos++
			}
		} else {
			for p.pos < len(p.input) && p.input[p.pos] != ' ' {
				p.pos++
			}
		}
		val := p.input[valStart:p.pos]

		pairs = append(pairs, inlinePair{key, sep, val})
	}
	return pairs, nil
}

func (p *inlineParser) skipSpace() {
	for p.pos < len(p.input) && (p.input[p.pos] == ' ' || p.input[p.pos] == '\n') {
		p.pos++
	}
}

func tokenizeRow(line string) []string {
	var tokens []string
	i := 0
	for i < len(line) {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		if i >= len(line) {
			break
		}
		if line[i] == '"' {
			end := i + 1
			for end < len(line) {
				if line[end] == '\\' && end+1 < len(line) {
					end += 2
				} else if line[end] == '"' {
					end++
					break
				} else {
					end++
				}
			}
			tokens = append(tokens, line[i+1:end-1])
			i = end
		} else if line[i] == '[' {
			end := i + 1
			for end < len(line) && line[end] != ']' {
				end++
			}
			tokens = append(tokens, line[i:end+1])
			i = end + 1
		} else {
			end := i
			for end < len(line) && line[end] != ' ' {
				end++
			}
			tokens = append(tokens, line[i:end])
			i = end
		}
	}
	return tokens
}

func setDeepField(dest reflect.Value, path, typ, valStr string) error {
	parts := strings.Split(path, ".")
	current := dest

	for i, part := range parts {
		current = deref(current)

		if i == len(parts)-1 {
			// Set value
			return setField(current, part, typ, valStr)
		}

		// Navigate deeper
		if current.Kind() == reflect.Map {
			if current.IsNil() {
				current.Set(reflect.MakeMap(current.Type()))
			}
			// Maps are tricky because we need to get the element to set it?
			// Or create a new temp value, set it, then put it back.
			// Currently setField handles map insertion.
			// But here we're mid-path.

			// Warning: Reflecting into Map keys for deep path is complex.
			// Simplified assumption: Maps in Go implementation are map[string]any or unmarshaling to struct.
			// If map[string]interface{}:

			keyVal := reflect.ValueOf(part)
			existing := current.MapIndex(keyVal)

			var nextVal reflect.Value
			if existing.IsValid() {
				// Copy to addressable if needed? MapIndex returns non-addressable.
				// For maps we probably need to handle intermediate Map/Struct creation.
				if existing.Kind() == reflect.Interface {
					existing = existing.Elem()
				}
				nextVal = existing
			} else {
				// Determine type for next level
				// If map type is map[string]interface{}, create map[string]interface{}
				mapElemType := current.Type().Elem()
				if mapElemType.Kind() == reflect.Interface {
					nextVal = reflect.MakeMap(reflect.TypeOf(map[string]any{}))
				} else if mapElemType.Kind() == reflect.Ptr {
					nextVal = reflect.New(mapElemType.Elem())
				} else {
					nextVal = reflect.New(mapElemType).Elem()
				}
			}

			// Recurse... but wait, 'nextVal' from MapIndex isn't addressable we can't set fields on it easiest way?
			// Go maps are refs. But reflect.Value of it...

			// This logic for deep map setting via reflection is hard.
			// Let's assume Structs for now or flat implementation limitations for maps?
			// For Map[String]Any, we can build a temp map.

			if current.Type().Key().Kind() != reflect.String {
				return fmt.Errorf("zoon: map key must be string for path %s", path)
			}

			// NOTE: We need to put it back into the map after modification if it's a struct/value type.
			// If it's a pointer or map, we can modify it directly.

			// Hack: Create a new map for next level if nil
			if !nextVal.IsValid() || (nextVal.Kind() == reflect.Map && nextVal.IsNil()) {
				// Try to guess type? map[string]any
				nextVal = reflect.MakeMap(reflect.TypeOf(map[string]any{}))
				current.SetMapIndex(keyVal, nextVal)
			}

			current = nextVal

		} else if current.Kind() == reflect.Struct {
			f := findField(current, part)
			if !f.IsValid() {
				return nil // Ignore unknown field
			}
			current = f
		} else {
			return fmt.Errorf("zoon: cannot traverse %s in %v", part, current.Kind())
		}
	}
	return nil
}

func setField(dest reflect.Value, name, typ, valStr string) error {
	dest = deref(dest)

	if dest.Kind() == reflect.Map {
		if dest.IsNil() {
			dest.Set(reflect.MakeMap(dest.Type()))
		}

		// Handle nested content for map values
		if strings.HasPrefix(valStr, "{") {
			// Recursive decode for map value
			inner := valStr[1 : len(valStr)-1]
			valType := dest.Type().Elem()
			valElem := reflect.New(valType).Elem()

			// If value is struct/map, use inline parser logic
			if valType.Kind() == reflect.Struct || valType.Kind() == reflect.Map {
				subParser := &inlineParser{input: inner}
				pairs, _ := subParser.parse()
				for _, p := range pairs {
					v := p.value
					if p.sep == "=" {
						v = strings.ReplaceAll(v, "_", " ")
					}
					setDeepField(valElem, p.key, "auto", v)
				}
				dest.SetMapIndex(reflect.ValueOf(name), valElem)
				return nil
			}
		}

		val := parsePrimitive(valStr, typ)
		// Check for nil
		if val == nil {
			dest.SetMapIndex(reflect.ValueOf(name), reflect.Zero(dest.Type().Elem()))
			return nil
		}

		dest.SetMapIndex(reflect.ValueOf(name), reflect.ValueOf(val))
		return nil
	}

	if dest.Kind() == reflect.Struct {
		field := findField(dest, name)
		if !field.IsValid() {
			return nil
		}

		if strings.HasPrefix(valStr, "{") {
			inner := valStr[1 : len(valStr)-1]
			subElem := reflect.New(field.Type()).Elem()
			subParser := &inlineParser{input: inner}
			pairs, _ := subParser.parse()
			for _, p := range pairs {
				v := p.value
				if p.sep == "=" {
					v = strings.ReplaceAll(v, "_", " ")
				}
				setDeepField(subElem, p.key, "auto", v)
			}
			field.Set(subElem)
			return nil
		}

		converted := parsePrimitive(valStr, typ)
		if converted == nil {
			// Explicit nil
			field.Set(reflect.Zero(field.Type()))
			return nil
		}

		rVal := reflect.ValueOf(converted)

		if rVal.Type().ConvertibleTo(field.Type()) {
			field.Set(rVal.Convert(field.Type()))
		}
		return nil
	}

	return nil
}

func deref(v reflect.Value) reflect.Value {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return v.Elem()
	}
	return v
}

func findField(strct reflect.Value, name string) reflect.Value {
	t := strct.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("zoon")
		if tag == "" {
			tag = f.Tag.Get("json")
		}
		if tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] == name {
				return strct.Field(i)
			}
		}
		if strings.EqualFold(f.Name, name) {
			return strct.Field(i)
		}
	}
	return reflect.Value{}
}

func parsePrimitive(s, typ string) any {
	if s == "~" {
		return nil
	}

	if typ == "i" || typ == "i+" {
		i, _ := strconv.Atoi(s)
		return i
	}
	if typ == "b" {
		return s == "1" || s == "y" || s == "true"
	}

	if s == "y" || s == "n" {
		return s == "y"
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	if s == "true" || s == "false" {
		return s == "true"
	}

	return strings.ReplaceAll(s, "_", " ")
}
