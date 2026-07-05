package simapp

const (
	wsTypeStatus          = "status"
	wsTypeMenu            = "menu"
	wsTypeDisplayText     = "display_text"
	wsTypeInput           = "input"
	wsTypeInkey           = "inkey"
	wsTypeConfirm         = "confirm"
	wsTypeError           = "error"
	wsTypeMenuSelection   = "menu_selection"
	wsTypeInputResponse   = "input_response"
	wsTypeInkeyResponse   = "inkey_response"
	wsTypeConfirmResponse = "confirm_response"
	wsTypeBack            = "back"
	wsTypeTerminate       = "terminate"
)

const (
	menuKindRoot       = "root"
	menuKindSelectItem = "select-item"
)

type wsMenuItem struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

type wsMenu struct {
	Kind          string       `json:"kind"`
	Title         string       `json:"title,omitempty"`
	Items         []wsMenuItem `json:"items"`
	DefaultItemID *int         `json:"defaultItemId,omitempty"`
	HelpAvailable bool         `json:"helpAvailable,omitempty"`
}

type wsServerMessage struct {
	Type              string       `json:"type"`
	Available         *bool        `json:"available,omitempty"`
	ProfileICCID      string       `json:"profileIccid,omitempty"`
	Menu              *wsMenu      `json:"menu,omitempty"`
	Kind              string       `json:"kind,omitempty"`
	Title             string       `json:"title,omitempty"`
	Items             []wsMenuItem `json:"items,omitempty"`
	DefaultItemID     *int         `json:"defaultItemId,omitempty"`
	HelpAvailable     bool         `json:"helpAvailable,omitempty"`
	Text              string       `json:"text,omitempty"`
	DefaultText       string       `json:"defaultText,omitempty"`
	MinLength         int          `json:"minLength,omitempty"`
	MaxLength         int          `json:"maxLength,omitempty"`
	HideInput         bool         `json:"hideInput,omitempty"`
	YesNo             bool         `json:"yesNo,omitempty"`
	HighPriority      bool         `json:"highPriority,omitempty"`
	UserClear         bool         `json:"userClear,omitempty"`
	ImmediateResponse bool         `json:"immediateResponse,omitempty"`
	Command           string       `json:"command,omitempty"`
	Message           string       `json:"message,omitempty"`
}

type wsClientMessage struct {
	Type          string `json:"type"`
	ItemID        int    `json:"itemId,omitempty"`
	HelpRequested bool   `json:"helpRequested,omitempty"`
	Text          string `json:"text,omitempty"`
	Accepted      bool   `json:"accepted,omitempty"`
}

func statusMessage(available bool, profileICCID string, menu *wsMenu) wsServerMessage {
	return wsServerMessage{
		Type:         wsTypeStatus,
		Available:    &available,
		ProfileICCID: profileICCID,
		Menu:         menu,
	}
}

func menuMessage(menu wsMenu) wsServerMessage {
	return wsServerMessage{
		Type:          wsTypeMenu,
		Kind:          menu.Kind,
		Title:         menu.Title,
		Items:         menu.Items,
		DefaultItemID: menu.DefaultItemID,
		HelpAvailable: menu.HelpAvailable,
	}
}
