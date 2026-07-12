package zohocliq

import miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"

// cliqSendRequest is the request body for Cliq message / message-card endpoints.
type cliqSendRequest struct {
	Text    string       `json:"text"`
	Bot     *cliqBot     `json:"bot,omitempty"`
	Card    *cliqCard    `json:"card,omitempty"`
	Slides  []cliqSlide  `json:"slides,omitempty"`
	Buttons []cliqButton `json:"buttons,omitempty"`
}

type cliqBot struct {
	Name  string `json:"name,omitempty"`
	Image string `json:"image,omitempty"`
}

type cliqCard struct {
	Title     string             `json:"title,omitempty"`
	Theme     string             `json:"theme,omitempty"`
	Thumbnail string             `json:"thumbnail,omitempty"`
	Sections  []cliqCardSection  `json:"sections,omitempty"`
}

// cliqCardSection is the modern-inline card body (labeled field rows).
// Zoho docs: card.sections[].fields[].{title,value} — this is the chrome
// that makes Channel Pull look like a card instead of a bullet wall.
type cliqCardSection struct {
	Title  string          `json:"title,omitempty"`
	Fields []cliqCardField `json:"fields"`
}

type cliqCardField struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

type cliqSlide struct {
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
	Data  any    `json:"data"`
}

type cliqTableData struct {
	Headers []string            `json:"headers"`
	Rows    []map[string]string `json:"rows"`
}

type cliqButton struct {
	Label  string           `json:"label"`
	Type   string           `json:"type,omitempty"`
	Key    string           `json:"key,omitempty"`
	Hint   string           `json:"hint,omitempty"`
	Action cliqButtonAction `json:"action"`
}

type cliqButtonAction struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

func buildCliqSendRequest(cmd *miov1.SendCommand, botName string) cliqSendRequest {
	body := cliqSendRequest{Text: cmd.GetText()}
	if rich := cmd.GetRichContent(); rich != nil {
		applyRichContent(&body, rich)
	}
	applyAttachmentSlides(&body, cmd.GetAttachments())
	if hasRichPayload(body) && botName != "" {
		body.Bot = &cliqBot{Name: botName}
	}
	return body
}

func hasRichPayload(body cliqSendRequest) bool {
	return body.Card != nil || len(body.Slides) > 0 || len(body.Buttons) > 0
}

func applyRichContent(body *cliqSendRequest, rich *miov1.RichContent) {
	if card := rich.GetCard(); card != nil {
		body.Card = &cliqCard{
			Title:     card.GetTitle(),
			Theme:     card.GetTheme(),
			Thumbnail: card.GetThumbnailUrl(),
		}
	}
	for _, block := range rich.GetBlocks() {
		if section, ok := richLabelToCardSection(block); ok {
			ensureCard(body)
			body.Card.Sections = append(body.Card.Sections, section)
			continue
		}
		if slide, ok := richBlockToCliqSlide(block); ok {
			body.Slides = append(body.Slides, slide)
		}
	}
	for _, button := range rich.GetButtons() {
		if b, ok := richButtonToCliq(button); ok {
			body.Buttons = append(body.Buttons, b)
		}
	}
}

func ensureCard(body *cliqSendRequest) {
	if body.Card == nil {
		body.Card = &cliqCard{Theme: "modern-inline"}
	}
	if body.Card.Theme == "" {
		body.Card.Theme = "modern-inline"
	}
}

func richLabelToCardSection(block *miov1.RichBlock) (cliqCardSection, bool) {
	label := block.GetLabel()
	if label == nil {
		return cliqCardSection{}, false
	}
	fields := make([]cliqCardField, 0, len(label.GetLabels()))
	for _, item := range label.GetLabels() {
		if item.GetKey() == "" && item.GetValue() == "" {
			continue
		}
		fields = append(fields, cliqCardField{
			Title: item.GetKey(),
			Value: item.GetValue(),
		})
	}
	if len(fields) == 0 {
		return cliqCardSection{}, false
	}
	return cliqCardSection{
		Title:  label.GetTitle(),
		Fields: fields,
	}, true
}

func richBlockToCliqSlide(block *miov1.RichBlock) (cliqSlide, bool) {
	switch {
	case block.GetText() != nil:
		text := block.GetText()
		if text.GetText() == "" {
			return cliqSlide{}, false
		}
		return cliqSlide{
			Type:  "text",
			Title: text.GetTitle(),
			Data:  text.GetText(),
		}, true
	case block.GetList() != nil:
		list := block.GetList()
		if len(list.GetItems()) == 0 {
			return cliqSlide{}, false
		}
		return cliqSlide{
			Type:  "list",
			Title: list.GetTitle(),
			Data:  list.GetItems(),
		}, true
	case block.GetTable() != nil:
		table := block.GetTable()
		if len(table.GetHeaders()) == 0 || len(table.GetRows()) == 0 {
			return cliqSlide{}, false
		}
		return cliqSlide{
			Type:  "table",
			Title: table.GetTitle(),
			Data: cliqTableData{
				Headers: table.GetHeaders(),
				Rows:    cliqTableRows(table.GetHeaders(), table.GetRows()),
			},
		}, true
	case block.GetLabel() != nil:
		// Labels prefer card.sections (see richLabelToCardSection). Keep slide
		// fallback only when applyRichContent did not already consume them.
		label := block.GetLabel()
		data := cliqLabelData(label.GetLabels())
		if len(data) == 0 {
			return cliqSlide{}, false
		}
		return cliqSlide{
			Type:  "label",
			Title: label.GetTitle(),
			Data:  data,
		}, true
	case block.GetImages() != nil:
		images := block.GetImages()
		if len(images.GetUrls()) == 0 {
			return cliqSlide{}, false
		}
		return cliqSlide{
			Type:  "images",
			Title: images.GetTitle(),
			Data:  images.GetUrls(),
		}, true
	default:
		return cliqSlide{}, false
	}
}

func cliqTableRows(headers []string, rows []*miov1.RichTableRow) []map[string]string {
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		cells := row.GetCells()
		item := make(map[string]string, len(headers))
		for i, header := range headers {
			value := ""
			if i < len(cells) {
				value = cells[i]
			}
			item[header] = value
		}
		out = append(out, item)
	}
	return out
}

func cliqLabelData(labels []*miov1.RichLabel) []map[string]string {
	out := make([]map[string]string, 0, len(labels))
	for _, label := range labels {
		if label.GetKey() == "" || label.GetValue() == "" {
			continue
		}
		out = append(out, map[string]string{label.GetKey(): label.GetValue()})
	}
	return out
}

func richButtonToCliq(button *miov1.RichButton) (cliqButton, bool) {
	if button.GetLabel() == "" || button.GetAction() == nil {
		return cliqButton{}, false
	}
	action, ok := richButtonActionToCliq(button.GetAction())
	if !ok {
		return cliqButton{}, false
	}
	return cliqButton{
		Label:  button.GetLabel(),
		Type:   cliqButtonType(button.GetStyle()),
		Key:    button.GetKey(),
		Hint:   button.GetHint(),
		Action: action,
	}, true
}

func richButtonActionToCliq(action *miov1.RichButtonAction) (cliqButtonAction, bool) {
	switch action.GetKind() {
	case miov1.RichButtonAction_KIND_OPEN_URL:
		if action.GetUrl() == "" {
			return cliqButtonAction{}, false
		}
		return cliqButtonAction{
			Type: "open.url",
			Data: map[string]any{"web": action.GetUrl()},
		}, true
	case miov1.RichButtonAction_KIND_PREVIEW_URL:
		if action.GetUrl() == "" {
			return cliqButtonAction{}, false
		}
		return cliqButtonAction{
			Type: "preview.url",
			Data: map[string]any{"url": action.GetUrl()},
		}, true
	case miov1.RichButtonAction_KIND_COPY:
		if action.GetText() == "" {
			return cliqButtonAction{}, false
		}
		return cliqButtonAction{
			Type: "copy",
			Data: map[string]any{"text": action.GetText()},
		}, true
	case miov1.RichButtonAction_KIND_INVOKE_FUNCTION:
		if action.GetFunctionName() == "" {
			return cliqButtonAction{}, false
		}
		data := map[string]any{"name": action.GetFunctionName()}
		if action.GetFunctionOwner() != "" {
			data["owner"] = action.GetFunctionOwner()
		}
		return cliqButtonAction{
			Type: "invoke.function",
			Data: data,
		}, true
	default:
		return cliqButtonAction{}, false
	}
}

func cliqButtonType(style miov1.RichButton_Style) string {
	switch style {
	case miov1.RichButton_STYLE_DANGER:
		return "-"
	default:
		return "+"
	}
}

func applyAttachmentSlides(body *cliqSendRequest, attachments []*miov1.Attachment) {
	imageURLs := make([]string, 0, len(attachments))
	fileLabels := make([]map[string]string, 0, len(attachments))
	for _, att := range attachments {
		u := att.GetUrl()
		if u == "" {
			continue
		}
		switch att.GetKind() {
		case miov1.Attachment_KIND_IMAGE:
			imageURLs = append(imageURLs, u)
		default:
			label := att.GetFilename()
			if label == "" {
				label = u
			}
			fileLabels = append(fileLabels, map[string]string{label: u})
		}
	}
	if len(imageURLs) > 0 {
		body.Slides = append(body.Slides, cliqSlide{
			Type:  "images",
			Title: "Images",
			Data:  imageURLs,
		})
	}
	if len(fileLabels) > 0 {
		body.Slides = append(body.Slides, cliqSlide{
			Type:  "label",
			Title: "Attachments",
			Data:  fileLabels,
		})
	}
}
