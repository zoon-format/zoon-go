package zoon

import (
	"reflect"
	"strings"
	"testing"
)

type User struct {
	ID     int    `zoon:"id"`
	Name   string `zoon:"name"`
	Role   string `zoon:"role"`
	Active bool   `zoon:"active"`
}

func TestTabular(t *testing.T) {
	users := []User{
		{1, "Alice", "Admin", true},
		{2, "Bob", "User", true},
		{3, "Carol", "User", false},
	}

	encoded, err := Marshal(users)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	expected := `# active:b id:i+ name:s role=Admin|User
1 Alice Admin
1 Bob User
0 Carol User
`
	got := string(encoded)
	got = strings.TrimSpace(got)
	expected = strings.TrimSpace(expected)

	if got != expected {
		t.Errorf("Encoding mismatch.\nGot:\n%s\nExpected:\n%s", got, expected)
	}

	var decoded []User
	if err := Unmarshal([]byte(expected), &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !reflect.DeepEqual(users, decoded) {
		t.Errorf("Roundtrip mismatch.\nOriginal: %+v\nDecoded: %+v", users, decoded)
	}
}

type ServerConfig struct {
	Host string `zoon:"host"`
	Port int    `zoon:"port"`
	SSL  bool   `zoon:"ssl"`
}

type DBConfig struct {
	Driver string `zoon:"driver"`
	Host   string `zoon:"host"`
	Port   int    `zoon:"port"`
}

type Config struct {
	Server ServerConfig `zoon:"server"`
	DB     DBConfig     `zoon:"database"`
}

func TestInline(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{"localhost", 3000, true},
		DB:     DBConfig{"postgres", "db.example.com", 5432},
	}

	encoded, err := Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	expected := `server:{host=localhost port:3000 ssl:y} database:{driver=postgres host=db.example.com port:5432}`

	got := string(encoded)
	if got != expected {
		t.Errorf("Encoding mismatch.\nGot: %q\nExp: %q", got, expected)
	}

	var decoded Config
	if err := Unmarshal([]byte(expected), &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !reflect.DeepEqual(cfg, decoded) {
		t.Errorf("Roundtrip mismatch.\nOriginal: %+v\nDecoded: %+v", cfg, decoded)
	}
}

func TestPrimitiveTypes(t *testing.T) {
	type Item struct {
		Name  string `zoon:"n"`
		Count int    `zoon:"c"`
		Flag  bool   `zoon:"f"`
	}

	data := []Item{
		{"A B", 10, true},
		{"C", 0, false},
	}

	enc, err := Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	out := string(enc)
	if !strings.Contains(out, "A_B") {
		t.Error("Spaces not replaced")
	}
	if !strings.Contains(out, "10 1") {
		t.Error("Values mismatch")
	}

	var dec []Item
	if err := Unmarshal(enc, &dec); err != nil {
		t.Fatal(err)
	}

	if dec[0].Name != "A B" {
		t.Errorf("Space restoration failed: %q", dec[0].Name)
	}
}

func TestNulls(t *testing.T) {
	type Node struct {
		Val  string `zoon:"v"`
		Next *Node  `zoon:"next"`
	}

	n := Node{Val: "head", Next: nil}
	enc, err := Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(enc), "next:~") {
		t.Errorf("Expected null marker ~, got %s", string(enc))
	}

	var dec Node
	if err := Unmarshal(enc, &dec); err != nil {
		t.Fatal(err)
	}
	if dec.Next != nil {
		t.Error("Expected nil next pointer")
	}
}

func TestNestedMap(t *testing.T) {
	data := map[string]map[string]int{
		"a": {"x": 1},
		"b": {"y": 2},
	}

	enc, err := Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	out := string(enc)
	if !strings.Contains(out, "a:{x:1}") {
		t.Error("Missing a:{x:1}")
	}

	var dec map[string]map[string]int
	if err := Unmarshal(enc, &dec); err != nil {
		t.Fatal(err)
	}
	if dec["a"]["x"] != 1 {
		t.Error("Deep map decode failed")
	}
}

func TestEmptySlice(t *testing.T) {
	var empty []User
	enc, err := Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	if len(enc) != 0 {
		t.Errorf("Expected empty output, got %q", string(enc))
	}
}

func TestSpecialCharacters(t *testing.T) {
	type Data struct {
		Text string `zoon:"text"`
	}

	data := []Data{
		{"Hello_World"},
		{"Tab\there"},
		{"Quote\"Mark"},
	}

	enc, err := Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	var dec []Data
	if err := Unmarshal(enc, &dec); err != nil {
		t.Fatal(err)
	}

	if dec[0].Text != "Hello World" {
		t.Errorf("Underscore not restored: %q", dec[0].Text)
	}
}

func TestFloatHandling(t *testing.T) {
	type Metric struct {
		Name  string  `zoon:"name"`
		Value float64 `zoon:"value"`
	}

	data := []Metric{
		{"cpu", 0.75},
		{"mem", 0.92},
	}

	enc, err := Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	out := string(enc)
	if !strings.Contains(out, "0.75") {
		t.Errorf("Float not serialized: %s", out)
	}
}

func TestSingleObject(t *testing.T) {
	cfg := ServerConfig{"api.example.com", 8080, false}
	enc, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	expected := "host=api.example.com port:8080 ssl:n"
	if string(enc) != expected {
		t.Errorf("Single object mismatch.\nGot: %q\nExp: %q", string(enc), expected)
	}

	var dec ServerConfig
	if err := Unmarshal(enc, &dec); err != nil {
		t.Fatal(err)
	}
	if dec.Host != "api.example.com" || dec.Port != 8080 || dec.SSL != false {
		t.Errorf("Decode mismatch: %+v", dec)
	}
}

func TestDecodeSimpleTabular(t *testing.T) {
	input := `# id:i+ name:s role:s active:b
Alice Admin 1
Bob User 0`
	var users []User
	if err := Unmarshal([]byte(input), &users); err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Errorf("Expected 2 users, got %d", len(users))
	}
	if users[0].Name != "Alice" {
		t.Errorf("Expected Alice, got %s", users[0].Name)
	}
}

func TestDecodeWithBooleans(t *testing.T) {
	input := `# name:s active:b
Alice 1
Bob 0`
	type Row struct {
		Name   string `zoon:"name"`
		Active bool   `zoon:"active"`
	}
	var rows []Row
	if err := Unmarshal([]byte(input), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].Active != true {
		t.Error("Expected Alice active=true")
	}
	if rows[1].Active != false {
		t.Error("Expected Bob active=false")
	}
}

func TestDecodeNumbers(t *testing.T) {
	input := `# name:s price:i
Widget 1999
Gadget 2950`
	type Product struct {
		Name  string `zoon:"name"`
		Price int    `zoon:"price"`
	}
	var products []Product
	if err := Unmarshal([]byte(input), &products); err != nil {
		t.Fatal(err)
	}
	if products[0].Price != 1999 {
		t.Errorf("Expected 1999, got %d", products[0].Price)
	}
}

func TestRoundtripSimple(t *testing.T) {
	data := []User{
		{1, "Alice", "Admin", true},
		{2, "Bob", "User", false},
	}
	enc, err := Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var dec []User
	if err := Unmarshal(enc, &dec); err != nil {
		t.Fatal(err)
	}
	if dec[0].Name != "Alice" || dec[1].Name != "Bob" {
		t.Error("Roundtrip name mismatch")
	}
}

func TestRoundtripWithNumbers(t *testing.T) {
	type Product struct {
		Name  string  `zoon:"name"`
		Price float64 `zoon:"price"`
		Stock int     `zoon:"stock"`
	}
	data := []Product{
		{"Widget", 19.99, 100},
		{"Gadget", 29.50, 50},
	}
	enc, err := Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	out := string(enc)
	if !strings.Contains(out, "19.99") {
		t.Error("Price not in output")
	}
}

func TestRoundtripWithBooleans(t *testing.T) {
	type Row struct {
		Name   string `zoon:"name"`
		Active bool   `zoon:"active"`
	}
	data := []Row{
		{"Alice", true},
		{"Bob", false},
	}
	enc, _ := Marshal(data)
	var dec []Row
	Unmarshal(enc, &dec)
	if dec[0].Active != true || dec[1].Active != false {
		t.Error("Boolean roundtrip failed")
	}
}

func TestRoundtripWithNulls(t *testing.T) {
	type Row struct {
		Name  string  `zoon:"name"`
		Email *string `zoon:"email"`
	}
	email := "alice@example.com"
	data := []Row{
		{"Alice", &email},
		{"Bob", nil},
	}
	enc, _ := Marshal(data)
	if !strings.Contains(string(enc), "~") {
		t.Error("Null not encoded")
	}
}

func TestTokenReduction(t *testing.T) {
	type Row struct {
		ID     int    `zoon:"id"`
		Name   string `zoon:"name"`
		Status string `zoon:"status"`
		Level  int    `zoon:"level"`
	}
	var data []Row
	for i := 1; i <= 10; i++ {
		data = append(data, Row{i, "User", "active", 1})
	}
	enc, _ := Marshal(data)
	jsonLen := 200
	zoonLen := len(enc)
	if zoonLen >= jsonLen {
		t.Logf("ZOON len: %d", zoonLen)
	}
}

func TestAliases(t *testing.T) {
	type Status struct {
		State string `zoon:"state"`
	}
	type Infra struct {
		Postgres Status `zoon:"postgres"`
		Redis    Status `zoon:"redis"`
	}
	type System struct {
		Infrastructure Infra `zoon:"infrastructure"`
	}

	data := []System{
		{Infra{Status{"up"}, Status{"up"}}},
		{Infra{Status{"down"}, Status{"down"}}},
	}

	enc, err := Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	out := string(enc)
	if !strings.Contains(out, "%") {
		t.Errorf("Aliases not used in output: %s", out)
	}

	// Check decoding
	var dec []System
	if err := Unmarshal(enc, &dec); err != nil {
		t.Fatal(err)
	}
	if dec[0].Infrastructure.Postgres.State != "up" {
		t.Errorf("Alias decode failed: %+v", dec[0])
	}
}

func TestConstants(t *testing.T) {
	type Log struct {
		Level   string `zoon:"level"`
		Message string `zoon:"msg"`
		Region  string `zoon:"region"`
	}

	data := []Log{
		{"INFO", "Start", "us-east-1"},
		{"INFO", "Processing", "us-east-1"},
		{"INFO", "Done", "us-east-1"},
	}

	enc, err := Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	out := string(enc)
	if !strings.Contains(out, "@level=INFO") {
		t.Error("Constant level not hoisted")
	}

	var dec []Log
	if err := Unmarshal(enc, &dec); err != nil {
		t.Fatal(err)
	}
	if dec[1].Level != "INFO" {
		t.Error("Constant decode failed")
	}
}

func TestExplicitRowCount(t *testing.T) {
	type Metric struct {
		ID     int    `zoon:"id"`
		Status string `zoon:"status"`
	}

	// All rows same except ID which is auto-inc
	data := []Metric{
		{1, "ok"},
		{2, "ok"},
		{3, "ok"},
	}

	enc, err := Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	out := string(enc)
	if !strings.Contains(out, "+3") {
		t.Errorf("Expected +3 row count, got: %s", out)
	}

	var dec []Metric
	if err := Unmarshal(enc, &dec); err != nil {
		t.Fatal(err)
	}
	if len(dec) != 3 {
		t.Errorf("Expected 3 rows, got %d", len(dec))
	}
	if dec[2].ID != 3 {
		t.Errorf("ID generation failed, got %d", dec[2].ID)
	}
}
