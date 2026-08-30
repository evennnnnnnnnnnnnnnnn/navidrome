package pwdstrength

// commonPasswords holds the base words that a password must not reduce to.
// Entries are the *normalized* form (see normalizeForBlocklist): lowercase, no
// surrounding punctuation, no leading/trailing digits, leet substitutions
// undone. So "password" here rejects "Password123", "P@ssw0rd!" and "passw0rd"
// alike, and only one entry is needed per family.
//
// This list is intentionally small. It is not a breach corpus and does not try
// to be — it covers the guesses that come first, which is what a strength meter
// is for. The length and variety rules do the rest of the work.
//
// The same list is mirrored in ui/src/utils/passwordStrength.js.
var commonPasswords = map[string]struct{}{}

func init() {
	for _, w := range commonPasswordList {
		commonPasswords[w] = struct{}{}
	}
}

var commonPasswordList = []string{
	// Passwords about passwords
	"password", "passwd", "pass", "pwd", "secret", "changeme", "default",
	"nopass", "letmein", "trustno", "whatever", "nothing", "unknown",

	// Accounts and roles
	"admin", "administrator", "root", "guest", "user", "login", "test", "temp",
	"demo", "sysadmin", "operator", "owner",

	// This application and its neighbours
	"navidrome", "subsonic", "airsonic", "feishin", "music", "media", "library",
	"server", "home", "jellyfin", "plex",

	// Keyboard runs
	"qwerty", "qwertyuiop", "asdf", "asdfgh", "zxcv", "zxcvbn", "qazwsx",
	"qweasd", "abc",

	// Perennials
	"iloveyou", "sunshine", "princess", "welcome", "monkey", "dragon", "master",
	"shadow", "freedom", "hunter", "harley", "ranger", "buster", "killer",
	"pepper", "ginger", "cookie", "flower", "orange", "banana", "chocolate",
	"purple", "silver", "golden", "diamond", "phoenix",

	// Sport and screens
	"football", "baseball", "soccer", "hockey", "basketball", "superman",
	"batman", "starwars", "pokemon", "minecraft", "naruto", "anime",

	// Brands people reuse
	"google", "facebook", "amazon", "apple", "samsung", "android", "windows",
	"linux", "ubuntu", "microsoft", "netflix", "spotify", "youtube", "github",

	// Names
	"michael", "jennifer", "jordan", "george", "andrew", "charlie", "thomas",
	"robert", "daniel", "matthew", "joshua", "ashley", "bailey", "jessica",
	"samantha", "tigger", "hello", "helloworld",

	// Calendar
	"summer", "winter", "spring", "autumn", "january", "february", "march",
	"april", "june", "july", "august", "september", "october", "november",
	"december", "monday", "friday",

	// Relevant to this library's subject matter, and therefore likelier here
	"japan", "japanese", "tokyo", "osaka", "kyoto", "sakura", "konnichiwa",
	"arigato", "senpai", "kawaii", "otaku", "nihon", "nihongo",
}

// CommonWords exposes the blocklist for the parity test that pins it to
// ui/src/utils/passwordCommonWords.json, which is what the web UI loads.
func CommonWords() []string {
	return commonPasswordList
}
