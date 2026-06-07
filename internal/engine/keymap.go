package engine

// asciiToKey maps a rune to the US-layout key that types it and whether Shift is
// required. It returns ok=false for runes it can't type. Only US QWERTY is
// supported; other layouts would need their own table.
func asciiToKey(r rune) (name string, shift bool, ok bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return "KEY_" + string(r-'a'+'A'), false, true
	case r >= 'A' && r <= 'Z':
		return "KEY_" + string(r), true, true
	}
	if k, found := runeKeys[r]; found {
		return k.name, k.shift, true
	}
	return "", false, false
}

type keySpec struct {
	name  string
	shift bool
}

// runeKeys covers digits, whitespace, and US-layout punctuation. Letters are
// handled arithmetically in asciiToKey.
var runeKeys = map[rune]keySpec{
	'1': {"KEY_1", false}, '2': {"KEY_2", false}, '3': {"KEY_3", false},
	'4': {"KEY_4", false}, '5': {"KEY_5", false}, '6': {"KEY_6", false},
	'7': {"KEY_7", false}, '8': {"KEY_8", false}, '9': {"KEY_9", false},
	'0': {"KEY_0", false},

	' ':  {"KEY_SPACE", false},
	'\n': {"KEY_ENTER", false},
	'\t': {"KEY_TAB", false},

	'-':  {"KEY_MINUS", false},
	'=':  {"KEY_EQUAL", false},
	'[':  {"KEY_LEFTBRACE", false},
	']':  {"KEY_RIGHTBRACE", false},
	';':  {"KEY_SEMICOLON", false},
	'\'': {"KEY_APOSTROPHE", false},
	'`':  {"KEY_GRAVE", false},
	'\\': {"KEY_BACKSLASH", false},
	',':  {"KEY_COMMA", false},
	'.':  {"KEY_DOT", false},
	'/':  {"KEY_SLASH", false},

	'!': {"KEY_1", true}, '@': {"KEY_2", true}, '#': {"KEY_3", true},
	'$': {"KEY_4", true}, '%': {"KEY_5", true}, '^': {"KEY_6", true},
	'&': {"KEY_7", true}, '*': {"KEY_8", true}, '(': {"KEY_9", true},
	')': {"KEY_0", true},

	'_': {"KEY_MINUS", true},
	'+': {"KEY_EQUAL", true},
	'{': {"KEY_LEFTBRACE", true},
	'}': {"KEY_RIGHTBRACE", true},
	':': {"KEY_SEMICOLON", true},
	'"': {"KEY_APOSTROPHE", true},
	'~': {"KEY_GRAVE", true},
	'|': {"KEY_BACKSLASH", true},
	'<': {"KEY_COMMA", true},
	'>': {"KEY_DOT", true},
	'?': {"KEY_SLASH", true},
}
