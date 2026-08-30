package server

import (
	"strings"

	"maunium.net/go/mautrix/event"

	"github.com/batuhan/easymatrix/internal/compat"
)

type userShape struct {
	ID            string
	Username      string
	PhoneNumber   string
	Email         string
	FullName      string
	ImgURL        string
	CannotMessage bool
	IsSelf        bool
}

func newCompatUser(shape userShape) compat.User {
	userID := strings.TrimSpace(shape.ID)
	fullName := strings.TrimSpace(shape.FullName)
	if fullName == "" {
		fullName = userID
	}
	return compat.User{
		ID:            userID,
		Username:      strings.TrimSpace(shape.Username),
		PhoneNumber:   strings.TrimSpace(shape.PhoneNumber),
		Email:         strings.TrimSpace(shape.Email),
		FullName:      fullName,
		ImgURL:        strings.TrimSpace(shape.ImgURL),
		CannotMessage: shape.CannotMessage,
		IsSelf:        shape.IsSelf,
	}
}

func userFromLocalBridgeProfile(remoteID string, profileData map[string]any) compat.User {
	return newCompatUser(userShape{
		ID:            remoteID,
		Username:      firstString(profileData, "username", "handle"),
		PhoneNumber:   firstString(profileData, "phone", "phone_number"),
		Email:         firstString(profileData, "email"),
		FullName:      firstString(profileData, "name", "display_name", "displayName"),
		ImgURL:        firstString(profileData, "avatar", "avatar_url"),
		CannotMessage: false,
		IsSelf:        true,
	})
}

func userFromMemberEvent(userID string, member event.MemberEventContent, selfUserID string) compat.User {
	phone := extractPhoneFromGhostID(userID)
	return newCompatUser(userShape{
		ID:            userID,
		FullName:      member.Displayname,
		ImgURL:        string(member.AvatarURL),
		PhoneNumber:   phone,
		CannotMessage: false,
		IsSelf:        userID == selfUserID,
	})
}

// extractPhoneFromGhostID tries to extract a phone number from bridge ghost user IDs.
// e.g., @whatsapp_8801581507724:beeper.local → +8801581507724
func extractPhoneFromGhostID(userID string) string {
	localpart := userID
	if strings.HasPrefix(localpart, "@") {
		localpart = localpart[1:]
	}
	if idx := strings.Index(localpart, ":"); idx > 0 {
		localpart = localpart[:idx]
	}
	// Only extract for known phone-based bridges.
	for _, prefix := range []string{"whatsapp_", "signal_", "gmessages_", "gvoice_"} {
		if strings.HasPrefix(localpart, prefix) {
			num := localpart[len(prefix):]
			// Validate it looks like a phone number (all digits).
			allDigits := true
			for _, ch := range num {
				if ch < '0' || ch > '9' {
					allDigits = false
					break
				}
			}
			if allDigits && len(num) >= 7 {
				return "+" + num
			}
		}
	}
	return ""
}
