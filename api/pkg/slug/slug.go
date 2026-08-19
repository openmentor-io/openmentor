package slug

import (
	"fmt"
	"regexp"
	"strings"
)

// cyrillicToLatin maps Cyrillic characters to Latin transliteration
var cyrillicToLatin = map[rune]string{
	'а': "a", 'А': "a",
	'б': "b", 'Б': "b",
	'в': "v", 'В': "v",
	'г': "g", 'Г': "g",
	'д': "d", 'Д': "d",
	'е': "e", 'Е': "e",
	'ё': "e", 'Ё': "e",
	'ж': "zh", 'Ж': "zh",
	'з': "z", 'З': "z",
	'и': "i", 'И': "i",
	'й': "y", 'Й': "y",
	'к': "k", 'К': "k",
	'л': "l", 'Л': "l",
	'м': "m", 'М': "m",
	'н': "n", 'Н': "n",
	'о': "o", 'О': "o",
	'п': "p", 'П': "p",
	'р': "r", 'Р': "r",
	'с': "s", 'С': "s",
	'т': "t", 'Т': "t",
	'у': "u", 'У': "u",
	'ф': "f", 'Ф': "f",
	'х': "h", 'Х': "h",
	'ц': "c", 'Ц': "c",
	'ч': "ch", 'Ч': "ch",
	'ш': "sh", 'Ш': "sh",
	'щ': "sh", 'Щ': "sh",
	'ъ': "", 'Ъ': "",
	'ы': "y", 'Ы': "y",
	'ь': "", 'Ь': "",
	'э': "e", 'Э': "e",
	'ю': "iu", 'Ю': "iu",
	'я': "ia", 'Я': "ia",
}

// GenerateMentorSlug generates a URL-friendly slug from mentor name and legacy ID
// Format: {transliterated-name}-{legacy-id}
// Example: "Иван Петров" + 42 -> "ivan-petrov-42"
func GenerateMentorSlug(name string, legacyID int) string {
	// Transliterate Cyrillic to Latin
	var result strings.Builder
	for _, char := range name {
		if latinChar, exists := cyrillicToLatin[char]; exists {
			result.WriteString(latinChar)
		} else {
			result.WriteRune(char)
		}
	}

	slug := result.String()

	// Remove non-alphabetic characters (except spaces)
	nonAlphaRegex := regexp.MustCompile(`[^a-zA-Z ]+`)
	slug = nonAlphaRegex.ReplaceAllString(slug, "")

	// Replace spaces with dashes
	slug = strings.ReplaceAll(slug, " ", "-")

	// Convert to lowercase
	slug = strings.ToLower(slug)

	// A generated slug obeys the same "mentor" rule a chosen one does (D87).
	// Registration's username field is optional, so without this the whole
	// prohibition is bypassed by omitting one JSON field: name "Anna Mentor"
	// would land on /mentor/anna-mentor-42.
	slug = dropMentorSegments(slug)

	// Append legacy ID for uniqueness
	return fmt.Sprintf("%s-%d", slug, legacyID)
}

// generatedSlugFallback stands in when a name consists of nothing BUT the
// stem. It only has to be neutral and free of the stem — the mentor can pick a
// real username afterwards through the D29 change flow.
const generatedSlugFallback = "member"

// dropMentorSegments removes the name segments that read as "mentor" from an
// already-lowercased, hyphen-joined name. Segments are dropped rather than the
// registration refused, because the input is somebody's actual name: "Anna
// Mentor" is a person, and turning her away is a worse outcome than the URL
// reading "anna-42". Returns the name unchanged when the stem is absent, so no
// existing name generates a different slug than it did before.
func dropMentorSegments(name string) string {
	if !isMentorDerivative(name) {
		return name
	}
	kept := make([]string, 0, strings.Count(name, "-")+1)
	for _, segment := range strings.Split(name, "-") {
		if segment != "" && !isMentorDerivative(segment) {
			kept = append(kept, segment)
		}
	}
	if len(kept) == 0 {
		return generatedSlugFallback
	}
	return strings.Join(kept, "-")
}
