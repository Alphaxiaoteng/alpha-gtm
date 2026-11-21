package egolite

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type PublishMedia struct {
	FilePath string `json:"filePath"`
	SHA256   string `json:"sha256"`
	Kind     string `json:"kind,omitempty"`
	Role     string `json:"role"`
}

type publishRuntimeTarget struct {
	PlatformCode      string         `json:"platformCode"`
	ContentType       string         `json:"contentType"`
	PrepareMode       string         `json:"prepareMode,omitempty"`
	ComposerURL       string         `json:"composerURL"`
	MetaPageID        string         `json:"metaPageID,omitempty"`
	MetaPageName      string         `json:"metaPageName,omitempty"`
	MetaInstagramID   string         `json:"metaInstagramID,omitempty"`
	MetaInstagramName string         `json:"metaInstagramName,omitempty"`
	PublishAt         string         `json:"publishAt,omitempty"`
	Audience          string         `json:"audience,omitempty"`
	Visibility        string         `json:"visibility,omitempty"`
	Playlist          string         `json:"playlist,omitempty"`
	AllowComments     bool           `json:"allow_comments"`
	AlteredContent    bool           `json:"altered_content"`
	AllowComment      bool           `json:"allow_comment"`
	AllowDuet         bool           `json:"allow_duet"`
	AllowStitch       bool           `json:"allow_stitch"`
	AIGenerated       bool           `json:"ai_generated"`
	AllowDownload     bool           `json:"allow_download"`
	Author            string         `json:"author,omitempty"`
	CommentEnabled    bool           `json:"comment_enabled"`
	DeclareOriginal   bool           `json:"declare_original"`
	AllowReprint      bool           `json:"allow_reprint"`
	PublishType       string         `json:"publish_type,omitempty"`
	Collection        string         `json:"collection,omitempty"`
	ContentSource     string         `json:"content_source,omitempty"`
	Title             string         `json:"title"`
	Caption           string         `json:"caption"`
	Body              string         `json:"body"`
	ExpectedBody      string         `json:"expectedBody,omitempty"`
	Tags              []string       `json:"tags"`
	Media             []PublishMedia `json:"media"`
	PreparePauseMS    int            `json:"preparePauseMs"`
	FinalPauseMS      int            `json:"finalPauseMs"`
}

type publishInteractionPhase string

const (
	publishInteractionPrepare     publishInteractionPhase = "prepare"
	publishInteractionFinalSubmit publishInteractionPhase = "final_submit"
)

const douyinReadbackContractJS = `
const normalizeDouyinTitle = (value) => String(value ?? "").replace(/[\u200B\uFEFF]/gu, "")
const normalizeDouyinBody = (value) => String(value ?? "")
  .replace(/\r\n?/g, "\n")
  .replace(/\u00A0/gu, " ")
  .replace(/[\u200B\uFEFF]/gu, "")
  .split("\n").map((line) => line.replace(/[ \t]+$/g, "")).join("\n")
  .replace(/^(?:[ \t]*\n)+/g, "")
  .replace(/(?:\n[ \t]*)+$/g, "")
const douyinCandidateMatches = (values, expected, normalize) => Array.isArray(values) &&
  values.some((value) => typeof value === "string" && normalize(value) === normalize(expected))
const douyinExpectedTopics = (tags) => [...new Set((tags || [])
  .map((tag) => String(tag ?? "").trim().replace(/^#+/, "").trim()).filter(Boolean))]
const douyinExactTopicSetMatches = (actual, expected) => {
  if (!Array.isArray(actual) || actual.length !== expected.length) return false
  const counts = new Map()
  for (const topic of actual) counts.set(topic, (counts.get(topic) || 0) + 1)
  return expected.every((topic) => counts.get(topic) === 1)
}
const douyinBodyAndTopicReadback = async (selector) => js("(() => {"+
  "const root=document.querySelector("+JSON.stringify(selector)+");if(!root)return {bodyCandidates:[],topics:[],invalid:true};"+
  "const topicSelector=\"[data-mention='#'],[data-topic-selected='true'],a.tiptap-topic[data-topic],[data-topic][contenteditable='false']\";const rawNodes=[...root.querySelectorAll(topicSelector)];const nodes=rawNodes.filter((node)=>!rawNodes.some((other)=>other!==node&&other.contains(node)));const topics=[];let invalid=false;"+
  "for(const node of nodes){let nativeTopicName = '';try{nativeTopicName=String(JSON.parse(node.getAttribute('data-topic') || '{}').name||'').replace(/^#+/u,'').trim();if(nativeTopicName)nativeTopicName='#'+nativeTopicName}catch{if(node.hasAttribute('data-topic'))invalid=true}const text=String(node.innerText||node.textContent||'').replace(/[\\u200B\\uFEFF]/gu,'').trim();const candidate=nativeTopicName || text;if(!candidate.startsWith('#')||!candidate.slice(1).trim())invalid=true;else topics.push(candidate.slice(1).trim())}"+
  "if(root.matches('input,textarea'))return {bodyCandidates:[root.value],topics,invalid};const liveBodyCandidates=[root.innerText,root.textContent];if(nodes.length===0)return {bodyCandidates:liveBodyCandidates,topics,invalid};const copy=root.cloneNode(true);copy.querySelectorAll(topicSelector).forEach((node)=>node.remove());"+
  "const container=document.createElement('div');const width=Math.max(root.getBoundingClientRect().width,1);container.setAttribute('aria-hidden','true');container.setAttribute('inert','');container.style.position='fixed';container.style.left='-100000px';container.style.top='0';container.style.width=width+'px';container.style.opacity='0';container.style.pointerEvents='none';container.style.userSelect='none';let renderedText='';let cloneTextContent='';try{container.appendChild(copy);document.body.appendChild(container);renderedText=copy.innerText;cloneTextContent=copy.textContent}finally{container.remove()}return {bodyCandidates:[renderedText,cloneTextContent],topics,invalid}})()")
`

// wechatVideoScheduleContractJS is shared by the prepare and submit-bound
// programs. Every DOM lookup is scoped to the single same-origin Video
// Channels editor iframe; the generated runtime never searches the top-level
// body for a schedule label and never uses coordinates.
const wechatVideoScheduleContractJS = `
const wechatVideoFramePrefix = "https://channels.weixin.qq.com/micro/content/post/create"
const wechatVideoExpectedSchedule = (publishAt) => {
  const instant = new Date(publishAt)
  if (Number.isNaN(instant.getTime()) || instant.getUTCSeconds() !== 0 || instant.getUTCMilliseconds() !== 0) return null
  const fields = Object.fromEntries(new Intl.DateTimeFormat('en-GB', {
    timeZone: 'Asia/Shanghai', calendar: 'gregory', year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
  }).formatToParts(instant).filter((part) => part.type !== 'literal').map((part) => [part.type, part.value]))
  const values = ['year', 'month', 'day', 'hour', 'minute'].map((name) => Number(fields[name]))
  if (!values.every(Number.isInteger)) return null
  const [year, month, day, hour, minute] = values
  if (month < 1 || month > 12 || day < 1 || day > 31 || hour < 0 || hour > 23 || minute < 0 || minute > 59) return null
  const pad = (value) => String(value).padStart(2, '0')
  const date = String(year).padStart(4, '0') + '-' + pad(month) + '-' + pad(day)
  const time = pad(hour) + ':' + pad(minute)
  return { date, time, dateTimeMinute: date + ' ' + time, dateTimeLocal: date + 'T' + time }
}
const uniqueWechatVideoScheduleControl = async (labels, marker) => js("(() => {" +
  "const prefix=" + JSON.stringify(wechatVideoFramePrefix) + ",labels=" + JSON.stringify(labels) + ",marker=" + JSON.stringify(marker) + ";" +
  "const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(prefix));" +
  "if(frames.length!==1)return frames.length>1?'__ambiguous__':'';let frameDocument=null;try{frameDocument=frames[0].contentDocument}catch{};if(!frameDocument)return '';" +
  "const visible=(node)=>{if(!node)return false;const style=frameDocument.defaultView?.getComputedStyle(node)||getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
  "const normalize=(value)=>String(value||'').normalize('NFKC').replace(/\\s+/g,' ').trim();const wanted=new Set(labels.map(normalize));" +
  "const labelText=(node)=>{const aria=node.getAttribute('aria-label');if(aria)return normalize(aria);const labelledBy=node.getAttribute('aria-labelledby');if(labelledBy){const text=labelledBy.split(/\\s+/).map((id)=>frameDocument.getElementById(id)?.textContent||'').join(' ');if(normalize(text))return normalize(text)}if(node.labels?.length)return normalize([...node.labels].map((label)=>label.textContent||'').join(' '));const label=node.closest('label');if(label)return normalize(label.textContent||'');return normalize(node.innerText||node.textContent||'')};" +
  "const raw=[...frameDocument.querySelectorAll(\"input[type='radio'],[role='radio'],label\")];const resolve=(node)=>{if(node.matches('label'))return node.control||node.querySelector(\"input[type='radio'],[role='radio']\")||node;return node};" +
  "const controls=[...new Set(raw.map(resolve))].filter((node)=>wanted.has(labelText(node))&&(visible(node)||visible(node.closest('label'))));const attribute='data-alpha-wechat-video-'+marker;for(const node of frameDocument.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);if(controls.length!==1)return controls.length>1?'__ambiguous__':'';controls[0].setAttribute(attribute,'true');return '['+attribute+'=\\\"true\\\"]';})()")
const readWechatVideoBoolean = async (selector) => js("(() => {" +
  "const prefix=" + JSON.stringify(wechatVideoFramePrefix) + ";const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(prefix));" +
  "if(frames.length!==1||!frames[0].contentDocument)return null;const frameDocument=frames[0].contentDocument,node=frameDocument.querySelector(" + JSON.stringify(selector) + ");if(!node)return null;" +
  "const state=(control)=>{if(!control)return null;if(control.checked===true||control.checked===false)return control.checked;for(const name of ['aria-checked','aria-selected','data-selected']){const value=control.getAttribute(name);if(value==='true'||value==='false')return value==='true'}if(control.hasAttribute('checked')||control.hasAttribute('selected'))return true;const className=String(control.className||'').toLowerCase();if(/(^|[-_])(selected|active|checked)([-_]|$)/.test(className))return true;return null};" +
  "const direct=state(node);if(direct!==null)return direct;const nested=[...node.querySelectorAll(\"input[type='radio'],[role='radio'],[aria-checked]\")];if(nested.length===1)return state(nested[0]);if(node.matches('label')&&node.control)return state(node.control);return null;})()")
const clickWechatVideoFrameControl = async (marker) => js("(() => {" +
  "const prefix=" + JSON.stringify(wechatVideoFramePrefix) + ",marker=" + JSON.stringify(marker) + ";const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(prefix));" +
  "if(frames.length!==1||!frames[0].contentDocument)return false;const node=frames[0].contentDocument.querySelector('[data-alpha-wechat-video-'+marker+'=\\\"true\\\"]');if(!node)return false;node.click();return true;})()")
const uniqueWechatVideoScheduleInput = async () => js("(() => {" +
  "const prefix=" + JSON.stringify(wechatVideoFramePrefix) + ";const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(prefix));" +
  "if(frames.length!==1||!frames[0].contentDocument)return frames.length>1?'__ambiguous__':'';const frameDocument=frames[0].contentDocument;const visible=(node)=>{const style=frameDocument.defaultView?.getComputedStyle(node)||getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
  "const selectors=[\"input[type='datetime-local']\",\"input[placeholder='选择发表时间']\",\"input[placeholder='选择发布时间']\",\"input[aria-label='发表时间']\"];let nodes=[...new Set(selectors.flatMap((selector)=>[...frameDocument.querySelectorAll(selector)]))].filter(visible);" +
  "if(nodes.length===0){const normalize=(value)=>String(value||'').normalize('NFKC').replace(/\\s+/g,' ').trim();const labels=[...frameDocument.querySelectorAll('label,[aria-label]')].filter((node)=>normalize(node.getAttribute('aria-label')||node.textContent)==='发表时间');const scoped=[];for(const label of labels){let current=label;for(let depth=0;current&&depth<4;depth+=1,current=current.parentElement){for(const node of current.querySelectorAll(\"input:not([type='checkbox']):not([type='radio'])\"))if(visible(node))scoped.push(node);}}nodes=[...new Set(scoped)];}" +
  "const attribute='data-alpha-wechat-video-schedule-input';for(const node of frameDocument.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);if(nodes.length!==1)return nodes.length>1?'__ambiguous__':'';nodes[0].setAttribute(attribute,'true');return '[data-alpha-wechat-video-schedule-input=\\\"true\\\"]';})()")
const setWechatVideoScheduleInput = async (selector, expected) => js("(() => {" +
  "const prefix=" + JSON.stringify(wechatVideoFramePrefix) + ",expected=" + JSON.stringify(expected) + ";const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(prefix));" +
  "if(frames.length!==1||!frames[0].contentDocument)return false;const node=frames[0].contentDocument.querySelector(" + JSON.stringify(selector) + ");if(!node)return false;const value=node.type==='datetime-local'?expected.dateTimeLocal:expected.dateTimeMinute;const prototype=Object.getPrototypeOf(node),descriptor=Object.getOwnPropertyDescriptor(prototype,'value');if(descriptor?.set)descriptor.set.call(node,value);else node.value=value;node.dispatchEvent(new Event('input',{bubbles:true}));node.dispatchEvent(new Event('change',{bubbles:true}));node.blur();const observed=String(node.value||node.getAttribute('value')||'').trim();return observed===expected.dateTimeLocal||observed===expected.dateTimeMinute;})()")
const wechatVideoScheduleReadbackStable = async (selector, expected, modeSelector) => {
  const allowed = new Set([expected.dateTimeMinute, expected.dateTimeLocal, expected.dateTimeMinute + ':00', expected.dateTimeLocal + ':00'])
  let previous = ''
  for (let attempt = 0; attempt < 2; attempt += 1) {
    if (await readWechatVideoBoolean(modeSelector) !== true) return false
    const readback = await js("(() => {" +
      "const prefix=" + JSON.stringify(wechatVideoFramePrefix) + ";const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(prefix));if(frames.length!==1||!frames[0].contentDocument)return '';const node=frames[0].contentDocument.querySelector(" + JSON.stringify(selector) + ");return String(node?.value||node?.getAttribute('value')||'').trim();})()")
    if (allowed.has(String(readback || '').trim())) {
      const signature = String(readback).trim()
      if (signature === previous) return true
      previous = signature
    }
    await new Promise((resolve) => setTimeout(resolve, 350))
  }
  return false
}
const uniqueWechatVideoSubmitField = async (selectors, marker) => js("(() => {" +
  "const prefix=" + JSON.stringify(wechatVideoFramePrefix) + ",selectors=" + JSON.stringify(selectors) + ",marker=" + JSON.stringify(marker) + ";const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(prefix));" +
  "if(frames.length!==1||!frames[0].contentDocument)return frames.length>1?'__ambiguous__':'';const frameDocument=frames[0].contentDocument;const visible=(node)=>{const style=frameDocument.defaultView?.getComputedStyle(node)||getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const nodes=[...new Set(selectors.flatMap((selector)=>[...frameDocument.querySelectorAll(selector)]))].filter(visible);const attribute='data-alpha-wechat-video-submit-'+marker;for(const node of frameDocument.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);if(nodes.length!==1)return nodes.length>1?'__ambiguous__':'';nodes[0].setAttribute(attribute,'true');return '['+attribute+'=\\\"true\\\"]';})()")
const uniqueWechatVideoSubmitFinalButton = async () => js("(() => {" +
  "const prefix=" + JSON.stringify(wechatVideoFramePrefix) + ";const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(prefix));if(frames.length!==1||!frames[0].contentDocument)return frames.length>1?'__ambiguous__':'';const frameDocument=frames[0].contentDocument;const visible=(node)=>{const style=frameDocument.defaultView?.getComputedStyle(node)||getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const buttons=[...frameDocument.querySelectorAll('button,[role=\"button\"]')].filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&(node.innerText||node.textContent||'').trim()==='发表');if(buttons.length!==1)return buttons.length>1?'__ambiguous__':'';buttons[0].setAttribute('data-alpha-wechat-video-submit-final','true');return '[data-alpha-wechat-video-submit-final=\\\"true\\\"]';})()")
const wechatVideoSubmitReadbackField = async (selector) => js("(() => {" +
  "const prefix=" + JSON.stringify(wechatVideoFramePrefix) + ";const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(prefix));if(frames.length!==1||!frames[0].contentDocument)return '';const node=frames[0].contentDocument.querySelector(" + JSON.stringify(selector) + ");return String(node?.value??node?.innerText??node?.textContent??'').replace(/\\r\\n?/g,'\\n');})()")
const wechatVideoSubmitBodyMatches = async (selector, expected, tags) => js("(() => {" +
  "const prefix=" + JSON.stringify(wechatVideoFramePrefix) + ",expected=" + JSON.stringify(expected) + ",tags=" + JSON.stringify(tags || []) + ";const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(prefix));if(frames.length!==1||!frames[0].contentDocument)return false;const node=frames[0].contentDocument.querySelector(" + JSON.stringify(selector) + ");if(!node)return false;const actual=String(node.value??node.innerText??node.textContent??'').replace(/\\r\\n?/g,'\\n');const cleanTags=tags.map((tag)=>String(tag||'').trim().replace(/^#+/,'')).filter(Boolean);const withTopics=cleanTags.length?expected.replace(/\\s+$/u,'')+'\\n\\n'+cleanTags.map((tag)=>'#'+tag).join(' '):expected;return new Set([expected,expected+'\\n',withTopics,withTopics+'\\n']).has(actual);})()")
const uniqueWechatVideoSubmitScheduleControl = async () => uniqueWechatVideoScheduleControl(["定时发表", "定时发布"], "submit-schedule-mode")
const uniqueWechatVideoSubmitScheduleInput = async () => uniqueWechatVideoScheduleInput()
const readWechatVideoSubmitBoolean = async (selector) => readWechatVideoBoolean(selector)
const wechatVideoSubmitScheduleReadbackStable = async (selector, expected, modeSelector) => wechatVideoScheduleReadbackStable(selector, expected, modeSelector)
`

func douyinExpectedBody(description string, tags []string) string {
	expected := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(strings.TrimLeft(tag, "#"))
		if tag != "" {
			expected[tag] = struct{}{}
		}
	}
	if len(expected) == 0 {
		return description
	}
	trimmedEnd := strings.TrimRight(description, " \t\r\n")
	lineStart := strings.LastIndexAny(trimmedEnd, "\r\n") + 1
	line := strings.TrimSpace(trimmedEnd[lineStart:])
	parts := strings.Fields(line)
	if len(parts) != len(expected) {
		return description
	}
	actual := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		if len(part) < 2 || part[0] != '#' || strings.Contains(part[1:], "#") {
			return description
		}
		tag := part[1:]
		if _, duplicate := actual[tag]; duplicate {
			return description
		}
		if _, wanted := expected[tag]; !wanted {
			return description
		}
		actual[tag] = struct{}{}
	}
	return strings.TrimRight(trimmedEnd[:lineStart], "\r\n")
}

func publishRuntimeTargetsWithExpectedDouyinBody(targets []publishRuntimeTarget) []publishRuntimeTarget {
	prepared := append([]publishRuntimeTarget(nil), targets...)
	for index := range prepared {
		if prepared[index].PlatformCode != "douyin" {
			continue
		}
		description := prepared[index].Body
		if description == "" {
			description = prepared[index].Caption
		}
		prepared[index].ExpectedBody = douyinExpectedBody(description, prepared[index].Tags)
	}
	return prepared
}

func publishInteractionPauseMillis(platformCode, contentType string, phase publishInteractionPhase) int {
	minimum, width := 450, 351
	if phase == publishInteractionFinalSubmit {
		minimum, width = 900, 601
	}
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(platformCode)) + "\x00" + strings.ToLower(strings.TrimSpace(contentType)) + "\x00" + string(phase)))
	seed := int(digest[0])<<8 | int(digest[1])
	return minimum + seed%width
}

// publishRuntimeTargetsForExecution owns pacing values so callers cannot make
// browser interactions instantaneous or turn them into unbounded waits.
func publishRuntimeTargetsForExecution(targets []publishRuntimeTarget) []publishRuntimeTarget {
	prepared := publishRuntimeTargetsWithExpectedDouyinBody(targets)
	for index := range prepared {
		prepared[index].PreparePauseMS = publishInteractionPauseMillis(prepared[index].PlatformCode, prepared[index].ContentType, publishInteractionPrepare)
		prepared[index].FinalPauseMS = publishInteractionPauseMillis(prepared[index].PlatformCode, prepared[index].ContentType, publishInteractionFinalSubmit)
	}
	return prepared
}

type publishRuntimeTargetResult struct {
	PlatformCode string                    `json:"platformCode"`
	Status       string                    `json:"status"`
	ErrorCode    string                    `json:"errorCode,omitempty"`
	Clicked      *bool                     `json:"clicked,omitempty"`
	Diagnostic   *PublishPrepareDiagnostic `json:"diagnostic,omitempty"`
}

type PublishPrepareDiagnostic struct {
	Branch   string                         `json:"branch"`
	Stage    string                         `json:"stage"`
	Topics   []string                       `json:"topics,omitempty"`
	BodyTail string                         `json:"bodyTail,omitempty"`
	Nodes    []PublishPrepareDiagnosticNode `json:"nodes,omitempty"`
}

type PublishPrepareDiagnosticNode struct {
	Tag        string                             `json:"tag"`
	Text       string                             `json:"text,omitempty"`
	Attributes PublishPrepareDiagnosticAttributes `json:"attributes,omitempty"`
}

type PublishPrepareDiagnosticAttributes struct {
	DataMention       string `json:"data-mention,omitempty"`
	DataTopicSelected string `json:"data-topic-selected,omitempty"`
	DataTopic         string `json:"data-topic,omitempty"`
	ContentEditable   string `json:"contenteditable,omitempty"`
}

type publishRuntimeResult struct {
	Done                bool                         `json:"done"`
	CompletionAttempted bool                         `json:"completionAttempted"`
	CompletionDone      bool                         `json:"completionDone"`
	Results             []publishRuntimeTargetResult `json:"results"`
}

func validateFrozenPublishMedia(media []PublishMedia) error {
	if len(media) > 35 {
		return fmt.Errorf("EGO_PUBLISH_MEDIA_INVALID")
	}
	for _, item := range media {
		path := strings.TrimSpace(item.FilePath)
		expected := strings.ToLower(strings.TrimSpace(item.SHA256))
		if path == "" || len(expected) != sha256.Size*2 {
			return fmt.Errorf("EGO_PUBLISH_MEDIA_INVALID")
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("EGO_PUBLISH_MEDIA_INVALID")
		}
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("EGO_PUBLISH_MEDIA_INVALID")
		}
		digest := sha256.New()
		_, copyErr := io.Copy(digest, file)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || hex.EncodeToString(digest.Sum(nil)) != expected {
			return fmt.Errorf("EGO_PUBLISH_MEDIA_HASH_CHANGED")
		}
	}
	return nil
}

func parseFrozenTags(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil, fmt.Errorf("EGO_PUBLISH_TAGS_INVALID")
	}
	if len(tags) > 20 {
		return nil, fmt.Errorf("EGO_PUBLISH_TAGS_INVALID")
	}
	clean := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(strings.TrimLeft(tag, "#"))
		if tag == "" || len([]rune(tag)) > 40 {
			continue
		}
		if _, duplicate := seen[tag]; duplicate {
			continue
		}
		seen[tag] = struct{}{}
		clean = append(clean, tag)
	}
	return clean, nil
}

const platformChallengeContractJS = `
const alphaPlatformChallengeVisible = async () => js("(() => {" +
  "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0&&node.getAttribute('aria-hidden')!=='true'};" +
  "const rawURL=String(location.href||'');let route='';try{const parsed=new URL(rawURL);route=parsed.pathname+parsed.search+parsed.hash}catch{}if(/captcha|verify|challenge|checkpoint/i.test(route))return true;" +
  "const frames=[...document.querySelectorAll(\"iframe[src*='captcha'],iframe[src*='verify'],iframe[src*='challenge'],iframe[src*='checkpoint']\")].filter(visible);if(frames.length>0)return true;" +
  "const roots=[...document.querySelectorAll(\"dialog,[role='dialog'],[aria-modal='true'],[class*='captcha'],[id*='captcha'],[class*='challenge'],[id*='challenge'],[class*='verification'],[id*='verification']\")].filter(visible);" +
  "const terms=/captcha|验证码|安全验证|扫码验证|security verification|checkpoint|challenge/i;const interactive=\"input,button,[role='button'],canvas,svg,img,[class*='qr'],[id*='qr']\";" +
  "return roots.some((root)=>terms.test((root.innerText||root.textContent||'').slice(0,3000))&&[...root.querySelectorAll(interactive)].some(visible));" +
  "})()")
`

func buildEgoPublishCompletionProgram(spaceID int64) string {
	return fmt.Sprintf(`const spaces = await listTaskSpaces()
const existingSpace = spaces.find((item) => Number(item.id) === %d)
if (!existingSpace) throw new Error(%s)
if (existingSpace.ownership === "user" || existingSpace.ownership === "agentDelegatedToUser") {
  throw new Error(%s)
}
await claimTaskSpace(%d)
const completed = await completeTaskSpace(%d, { keep: true })
cliLog(%q + JSON.stringify({
  done: completed?.done === true,
  skipped: String(completed?.skipped ?? ""),
}))
`, spaceID, jsErrorLiteral(ErrSpaceBindingMissing), jsErrorLiteral(ErrSpaceUserControlled), spaceID, spaceID, resultPrefix)
}

func buildEgoPublishPrepareProgram(spaceID int64, targets []publishRuntimeTarget) string {
	targetsJSON, _ := json.Marshal(publishRuntimeTargetsForExecution(targets))
	return fmt.Sprintf(`const spaces = await listTaskSpaces()
const existingSpace = spaces.find((item) => Number(item.id) === %d)
if (!existingSpace) throw new Error(%s)
if (existingSpace.ownership === "user") {
  throw new Error(%s)
} else if (existingSpace.ownership === "agentDelegatedToUser") {
  await takeOverTaskSpace(%d)
} else {
  await useOrCreateTaskSpace(%d)
}
const publishTargets = %s
`+platformChallengeContractJS+douyinReadbackContractJS+wechatVideoScheduleContractJS+`
const preparationResults = []
const publishDeadlineAt = Date.now() + 25000
const remainingPublishBudget = () => publishDeadlineAt - Date.now()
const alphaInteractionPause = async (milliseconds, minimum, maximum) => {
  const pause = Number(milliseconds)
  if (!Number.isInteger(pause) || pause < minimum || pause > maximum) throw new Error("publish_interaction_pacing_invalid")
  await new Promise((resolve) => setTimeout(resolve, pause))
}
const visible = (element) => {
  if (!element) return false
  const style = getComputedStyle(element)
  const rect = element.getBoundingClientRect()
  return style.visibility !== "hidden" && style.display !== "none" && rect.width > 0 && rect.height > 0
}
const douyinRoute = (value, pathname) => {
  try {
    const parsed = new URL(String(value || ""))
    return parsed.protocol === "https:" && parsed.hostname === "creator.douyin.com" && parsed.pathname === pathname
  } catch { return false }
}
const fillRichEditor = async (selector, value) => js("(() => {" +
  "const node=document.querySelector(" + JSON.stringify(selector) + ");" +
  "const value=" + JSON.stringify(value) + ";" +
	  "if(!node)return false;node.focus();node.textContent=value;node.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:value}));" +
	  "const selection=window.getSelection();if(!selection)return false;try{const range=document.createRange();range.selectNodeContents(node);range.collapse(false);selection.removeAllRanges();selection.addRange(range)}catch{return false}" +
  "if(selection.rangeCount!==1||!selection.isCollapsed||!node.contains(selection.anchorNode)||!node.contains(selection.focusNode))return false;" +
  "const actual=(node.innerText||node.textContent||'').replace(/\\r\\n?/g,'\\n');" +
  "return actual===value||actual===value+'\\n';})()")
const fillDouyinRichEditor = async (selector, value) => {
  let selectionReady = false
  try {
    selectionReady = await js("(() => {" +
      "const node=document.querySelector(" + JSON.stringify(selector) + ");" +
      "if(!node||node.getAttribute('contenteditable')!=='true')return false;" +
      "node.focus();const selection=window.getSelection();if(!selection)return false;" +
      "try{const range=document.createRange();range.selectNodeContents(node);selection.removeAllRanges();selection.addRange(range)}catch{return false}" +
      "const inside=(anchor)=>Boolean(anchor&&(anchor===node||node.contains(anchor)));" +
      "return selection.rangeCount===1&&inside(selection.anchorNode)&&inside(selection.focusNode);" +
    "})()")
  } catch {
    throw new Error("douyin_body_selection_failed")
  }
  if (!selectionReady) throw new Error("douyin_body_selection_failed")

  try {
    const typed = await typeText(value)
    if (typed === false) throw new Error("douyin_body_input_failed")
  } catch {
    throw new Error("douyin_body_input_failed")
  }

  let previousSignature = ""
  for (let attempt = 0; attempt < 2; attempt += 1) {
    let readback
    try {
      readback = await douyinBodyAndTopicReadback(selector)
    } catch {
      throw new Error("douyin_body_readback_runtime_failed")
    }
    if (!readback || readback.invalid || !Array.isArray(readback.bodyCandidates)) {
      throw new Error("douyin_body_readback_invalid")
    }
    if (!douyinCandidateMatches(readback.bodyCandidates, value, normalizeDouyinBody)) {
      throw new Error("douyin_body_readback_mismatch")
    }
    const matchingValues = [...new Set(readback.bodyCandidates
      .filter((candidate) => typeof candidate === "string")
      .map(normalizeDouyinBody)
      .filter((candidate) => candidate === normalizeDouyinBody(value)))]
    if (!matchingValues.length) throw new Error("douyin_body_readback_mismatch")
    const signature = JSON.stringify(matchingValues)
    if (previousSignature && signature !== previousSignature) {
      throw new Error("douyin_body_readback_unstable")
    }
    if (previousSignature) return true
    previousSignature = signature
    await new Promise((resolve) => setTimeout(resolve, 350))
  }
  throw new Error("douyin_body_readback_unstable")
}
const uniqueDouyinScheduleControl = async (labels, marker) => {
  return js("(() => {" +
    "const labels=" + JSON.stringify(labels) + ",marker=" + JSON.stringify(marker) + ";" +
    "const attribute='data-alpha-douyin-schedule-'+marker;for(const node of document.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);" +
    "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
    "const normalize=(value)=>String(value||'').normalize('NFKC').replace(/\\s+/g,' ').trim();const wanted=new Set(labels.map(normalize));" +
    "const controlLabel=(node)=>{const aria=node.getAttribute('aria-label');if(aria)return normalize(aria);const labelledBy=node.getAttribute('aria-labelledby');if(labelledBy){const text=labelledBy.split(/\\s+/).map((id)=>document.getElementById(id)?.textContent||'').join(' ');if(normalize(text))return normalize(text)}if(node.labels?.length)return normalize([...node.labels].map((label)=>label.textContent||'').join(' '));const label=node.closest('label');if(label)return normalize(label.textContent||'');return normalize(node.innerText||node.textContent||'')};" +
    "const controls=[...document.querySelectorAll(\"input[type='radio'],input[type='checkbox'],[role='radio'],[role='checkbox']\")].filter((node)=>wanted.has(controlLabel(node))&&(visible(node)||[...(node.labels||[])].some(visible)||visible(node.closest('label'))));" +
    "if(controls.length!==1)return '';controls[0].setAttribute('data-alpha-douyin-schedule-'+marker,'true');return '[data-alpha-douyin-schedule-'+marker+'=\\\"true\\\"]';})()")
}
const douyinScheduleControlChecked = async (selector) => js("(() => {const node=document.querySelector(" + JSON.stringify(selector) + ");if(!node)return null;const stateSelector=\"input[type='radio'],input[type='checkbox'],[role='radio'],[role='checkbox'],[aria-checked]\";const state=(control)=>{if(!control)return null;if(control.checked===true||control.checked===false)return control.checked;for(const name of ['aria-checked','aria-selected','data-selected']){const value=control.getAttribute(name);if(value==='true'||value==='false')return value==='true'}return control.hasAttribute('checked')||control.hasAttribute('selected')||control.classList.contains('selected')||control.classList.contains('active')||control.classList.contains('checked')};if(node.matches(stateSelector))return state(node);if(node.tagName==='LABEL'&&node.htmlFor){const linked=document.getElementById(node.htmlFor);if(linked)return state(linked)}const nested=[...node.querySelectorAll(stateSelector)];if(nested.length===1)return state(nested[0]);let scope=node.parentElement;for(let depth=0;scope&&depth<5;depth+=1,scope=scope.parentElement){const controls=[...scope.querySelectorAll(stateSelector)];if(controls.length===1)return state(controls[0])}return null;})()")
const uniqueDouyinScheduleTimeInput = async () => js("(() => {for(const node of document.querySelectorAll('[data-alpha-douyin-schedule-time]'))node.removeAttribute('data-alpha-douyin-schedule-time');const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const inputs=[...document.querySelectorAll(\"input[type='text'][placeholder='日期和时间'][format='yyyy-MM-dd HH:mm']\")].filter(visible);if(inputs.length!==1)return '';inputs[0].setAttribute('data-alpha-douyin-schedule-time','true');return '[data-alpha-douyin-schedule-time=\\\"true\\\"]';})()")
const douyinExpectedSchedule = (publishAt) => { const date = new Date(publishAt); if (Number.isNaN(date.getTime()) || date.getSeconds() !== 0 || date.getMilliseconds() !== 0) return ''; const pad = (value) => String(value).padStart(2, '0'); return date.getFullYear() + '-' + pad(date.getMonth() + 1) + '-' + pad(date.getDate()) + ' ' + pad(date.getHours()) + ':' + pad(date.getMinutes()) }
const uniqueDouyinFinalButton = async (marker) => js("(() => {const marker=" + JSON.stringify(marker) + ";const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const buttons=[...document.querySelectorAll('button')].filter((node)=>visible(node)&&(node.innerText||node.textContent||'').trim()==='发布');if(buttons.length!==1)return '';buttons[0].setAttribute('data-alpha-douyin-schedule-'+marker,'true');return '[data-alpha-douyin-schedule-'+marker+'=\\\"true\\\"]';})()")
const douyinFinalButtonEnabled = async (selector) => js("(() => {const node=document.querySelector(" + JSON.stringify(selector) + ");return Boolean(node&&!node.disabled&&node.getAttribute('aria-disabled')!=='true')})()")
const douyinTopicContext = { branch: "", stage: "", topics: [] }
const boundedDouyinTopicDiagnostic = async (bodySelector) => {
  const context = { branch: douyinTopicContext.branch, stage: douyinTopicContext.stage, topics: douyinTopicContext.topics.slice(0, 4) }
  if (!bodySelector) {
    const bounded = context
    return new TextEncoder().encode(JSON.stringify(bounded)).length <= 4096 ? bounded : undefined
  }
  let diagnostic
  try {
    diagnostic = await js("(() => {"+
      "const root=document.querySelector("+JSON.stringify(bodySelector)+");if(!root)return null;const normalize=(value)=>Array.from(String(value||'').replace(/[\\u200B\\uFEFF]/gu,'').replace(/\\u00A0/gu,' ').replace(/\\s+/gu,' ').trim());const text=(value)=>normalize(value).slice(0,80).join('');"+
      "const bodyText=normalize(root.value??root.innerText??root.textContent??'');const bodyTail=bodyText.slice(-80).join('');const selectors=\"[data-mention],[data-topic-selected],[data-topic],[contenteditable=false],a\";"+
      "const nodes=[...root.querySelectorAll(selectors)].slice(0,12).map((node)=>{const attributes={};for(const name of ['data-mention','data-topic-selected','data-topic','contenteditable']){if(node.hasAttribute(name))attributes[name]=text(node.getAttribute(name))}return {tag:String(node.tagName||'').toLowerCase(),text:text(node.innerText||node.textContent||''),attributes}});return {bodyTail,nodes}})()")
  } catch {}
  if (!diagnostic) {
    const bounded = context
    return new TextEncoder().encode(JSON.stringify(bounded)).length <= 4096 ? bounded : undefined
  }
  const bounded = { branch: context.branch, stage: context.stage, topics: context.topics, bodyTail: diagnostic.bodyTail, nodes: diagnostic.nodes }
  return new TextEncoder().encode(JSON.stringify(bounded)).length <= 4096 ? bounded : undefined
}
const commitDouyinNativeTopics = async (bodySelector, tags, expectedBody) => {
  const expected = douyinExpectedTopics(tags)
  if (expected.length > 3) throw new Error("douyin_topic_limit_exceeded")
  const typeDouyinInlinePickerTopic = async (tag) => {
    for (const character of [...tag]) {
      try {
        await typeText(character)
      } catch {
        throw new Error("douyin_topic_input_failed")
      }
      await new Promise((resolve) => setTimeout(resolve, 80))
    }
    return true
  }
  const douyinExactTopicSuggestionSelector = async (expectedValue) => {
    try {
      return await js("(() => {"+
        "for(const node of document.querySelectorAll('[data-alpha-douyin-topic-suggestion]'))node.removeAttribute('data-alpha-douyin-topic-suggestion');"+
        "const body=document.querySelector("+JSON.stringify(bodySelector)+");const expected="+JSON.stringify(expectedValue)+";const normalize=(value)=>String(value||'').normalize('NFKC').replace(/\\s+/gu,' ').trim();const matches=(value)=>{const candidate=normalize(value),labels=[normalize(expected),normalize('#'+expected)];return labels.some((label)=>candidate===label||candidate.startsWith(label+' '))};"+
        "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};"+
        "const official=[...document.querySelectorAll('#creator-editor-topic-container .item')].filter((item)=>{if(!visible(item))return false;const name=item.querySelector('.name');return Boolean(name&&matches(name.innerText||name.textContent||''))});"+
        "if(official.length===1){official[0].setAttribute('data-alpha-douyin-topic-suggestion','true');return '[data-alpha-douyin-topic-suggestion=\\\"true\\\"]'}if(official.length>1)return '__ambiguous__';"+
        "const semanticSelectors=['[role=option]','[role=listbox] li','li','button','a'];const semanticCandidates=[...new Set(semanticSelectors.flatMap((selector)=>[...document.querySelectorAll(selector)]))].filter((node)=>visible(node)&&!body?.contains(node)&&matches(node.innerText||node.textContent||''));"+
        "if(semanticCandidates.length!==1)return semanticCandidates.length>1?'__ambiguous__':'';semanticCandidates[0].setAttribute('data-alpha-douyin-topic-suggestion','true');return '[data-alpha-douyin-topic-suggestion=\\\"true\\\"]'})()")
    } catch {
      throw new Error("douyin_topic_suggestion_discovery_runtime_failed")
    }
  }
  const stableDouyinBodyReadback = async (selector, expectedValue) => {
    const expectedNormalized = normalizeDouyinBody(expectedValue)
    let previousSignature = ""
    for (let attempt = 0; attempt < 3; attempt += 1) {
      let readback
      try {
        readback = await douyinBodyAndTopicReadback(selector)
      } catch {
        throw new Error("douyin_topic_readback_runtime_failed")
      }
      if (!readback || readback.invalid) throw new Error("douyin_topic_body_readback_invalid")
      const values = (readback.bodyCandidates || []).filter((value) => typeof value === "string").map(normalizeDouyinBody)
      const matchingValues = [...new Set(values.filter((value) => value === expectedNormalized))]
      if (!matchingValues.length) throw new Error("douyin_topic_body_readback_mismatch")
      const signature = JSON.stringify(matchingValues)
      if (previousSignature && signature !== previousSignature) throw new Error("douyin_topic_body_readback_drift")
      if (previousSignature) return true
      previousSignature = signature
      await new Promise((resolve) => setTimeout(resolve, 100))
    }
    throw new Error("douyin_topic_body_readback_unstable")
  }
  douyinTopicContext.branch = "trigger"
  douyinTopicContext.stage = "precondition"
  await stableDouyinBodyReadback(bodySelector, expectedBody)
  for (let index = 0; index < expected.length; index += 1) {
    const tag = expected[index]
    douyinTopicContext.branch = "trigger"
    douyinTopicContext.stage = "trigger"
    let triggerSelector
    try {
      triggerSelector = await js("(() => {"+
        "for(const node of document.querySelectorAll('[data-alpha-douyin-topic-trigger]'))node.removeAttribute('data-alpha-douyin-topic-trigger');"+
        "const body=document.querySelector("+JSON.stringify(bodySelector)+");const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};"+
        "const names=new Set(['#添加话题','添加话题']);const exact=(node)=>names.has(String(node?.innerText||node?.textContent||'').trim());"+
        "const leaves=[...document.querySelectorAll('body *')].filter((node)=>visible(node)&&node.children.length===0&&!body?.contains(node)&&exact(node));"+
        "const candidates=[...new Set(leaves.map((node)=>{const reviewed=node.closest('button,a,[role=button],[role=link]');return reviewed&&visible(reviewed)&&exact(reviewed)?reviewed:node}))];"+
        "if(candidates.length!==1)return candidates.length>1?'__ambiguous__':'';candidates[0].setAttribute('data-alpha-douyin-topic-trigger','true');return '[data-alpha-douyin-topic-trigger=\"true\"]'})()")
    } catch {
      throw new Error("douyin_topic_trigger_discovery_runtime_failed")
    }
    if (!triggerSelector || triggerSelector === "__ambiguous__") throw new Error("douyin_topic_trigger_missing_or_ambiguous")
    try {
      await click(triggerSelector, { label: "open exact Douyin topic picker" })
    } catch {
      throw new Error("douyin_topic_trigger_click_failed")
    }
    await new Promise((resolve) => setTimeout(resolve, 300))
    let nativeFocus
    try {
      nativeFocus = await js("(() => {const root=document.querySelector("+JSON.stringify(bodySelector)+");const active=document.activeElement;const selection=window.getSelection();return Boolean(root&&active&&(active===root||root.contains(active))&&selection?.anchorNode&&root.contains(selection.anchorNode))})()")
    } catch {
      throw new Error("douyin_topic_focus_readback_runtime_failed")
    }
    if (!nativeFocus) throw new Error("douyin_topic_native_focus_missing")
    await typeDouyinInlinePickerTopic(tag)
    douyinTopicContext.stage = "suggestion"
    let suggestionSelector = ""
    for (let suggestionAttempt = 0; suggestionAttempt < 6; suggestionAttempt += 1) {
      await new Promise((resolve) => setTimeout(resolve, 500))
      try {
        suggestionSelector = await douyinExactTopicSuggestionSelector(tag)
      } catch {
        throw new Error("douyin_topic_suggestion_discovery_runtime_failed")
      }
      if (suggestionSelector === "__ambiguous__") throw new Error("douyin_topic_suggestion_ambiguous")
      if (suggestionSelector) break
    }
    if (!suggestionSelector) throw new Error("douyin_topic_suggestion_missing")
    douyinTopicContext.branch = "suggestion_click"
    try {
      await click(suggestionSelector, { label: "select exact Douyin topic" })
    } catch {
      throw new Error("douyin_topic_suggestion_click_failed")
    }
    douyinTopicContext.stage = "native_commit"
    let nativeCommitted = false
    let consecutiveNativeReadbacks = 0
    for (let attempt = 0; attempt < 6; attempt += 1) {
      await new Promise((resolve) => setTimeout(resolve, 500))
      let committed
      try {
        committed = await douyinBodyAndTopicReadback(bodySelector)
      } catch {
        throw new Error("douyin_topic_readback_runtime_failed")
      }
      const readbackMatches = Boolean(committed &&
        !committed.invalid &&
        douyinExactTopicSetMatches(committed?.topics, expected.slice(0, index + 1)) &&
        douyinCandidateMatches(committed?.bodyCandidates, expectedBody, normalizeDouyinBody))
      if (readbackMatches) {
        consecutiveNativeReadbacks += 1
        if (consecutiveNativeReadbacks >= 2) {
          nativeCommitted = true
          break
        }
      } else {
        consecutiveNativeReadbacks = 0
      }
    }
    if (!nativeCommitted) {
      throw new Error("douyin_topic_native_commit_mismatch")
    }
  }
  return expected
}
const reviewedPrepareErrorCode = (platformCode, error) => {
  const message = String(error?.message || error || '')
  if (platformCode === "douyin") {
    if (/douyin_home_route_unverified/i.test(message)) return "EGO_DOUYIN_HOME_ROUTE_UNVERIFIED"
    if (/douyin_graph_upload_route_missing/i.test(message)) return "EGO_DOUYIN_ENTRY_OR_FALLBACK_FAILED"
    if (/douyin_image_file_input_missing_or_ambiguous/i.test(message)) return "EGO_DOUYIN_IMAGE_INPUT_MISSING"
	if (/douyin_image_file_input_multiple_unsupported/i.test(message)) return "EGO_DOUYIN_IMAGE_INPUT_MULTIPLE_UNSUPPORTED"
	if (/douyin_image_upload_file_readback_mismatch/i.test(message)) return "EGO_DOUYIN_IMAGE_UPLOAD_READBACK_MISMATCH"
	if (/douyin_upload_still_in_progress/i.test(message)) return "EGO_DOUYIN_UPLOAD_IN_PROGRESS"
    if (/douyin_required_image_route_missing/i.test(message)) return "EGO_DOUYIN_REQUIRED_IMAGE_ROUTE_MISSING"
    if (/upload/i.test(message)) return "EGO_DOUYIN_UPLOAD_FAILED"
    if (/composer_fields_missing/i.test(message)) return "EGO_DOUYIN_REQUIRED_CONTROL_MISSING"
    if (/douyin_body_selection_failed/i.test(message)) return "EGO_DOUYIN_BODY_SELECTION_FAILED"
    if (/douyin_body_input_failed/i.test(message)) return "EGO_DOUYIN_BODY_INPUT_FAILED"
    if (/douyin_body_readback_invalid/i.test(message)) return "EGO_DOUYIN_BODY_READBACK_INVALID"
    if (/douyin_body_readback_unstable/i.test(message)) return "EGO_DOUYIN_BODY_READBACK_UNSTABLE"
    if (/body_fill_failed/i.test(message)) return "EGO_DOUYIN_BODY_READBACK_MISMATCH"
    if (/douyin_body_readback_mismatch/i.test(message)) return "EGO_DOUYIN_BODY_READBACK_MISMATCH"
    if (/douyin_topic_trigger_discovery_runtime_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_TRIGGER_DISCOVERY_RUNTIME_FAILED"
    if (/douyin_topic_focus_readback_runtime_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_FOCUS_READBACK_RUNTIME_FAILED"
    if (/douyin_topic_suggestion_discovery_runtime_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_SUGGESTION_DISCOVERY_RUNTIME_FAILED"
    if (/douyin_topic_precondition_runtime_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_PRECONDITION_RUNTIME_FAILED"
    if (/douyin_topic_trigger_|douyin_topic_native_focus/i.test(message)) return "EGO_DOUYIN_TOPIC_TRIGGER_FAILED"
    if (/douyin_topic_suggestion_/i.test(message)) return "EGO_DOUYIN_TOPIC_SUGGESTION_FAILED"
    if (/douyin_topic_commit_precondition_/i.test(message)) return "EGO_DOUYIN_TOPIC_COMMIT_PRECONDITION_FAILED"
    if (/douyin_topic_native_commit_/i.test(message)) return "EGO_DOUYIN_TOPIC_NATIVE_COMMIT_FAILED"
    if (/douyin_topic_body_readback_(invalid|mismatch|drift|unstable)/i.test(message)) return "EGO_DOUYIN_TOPIC_BODY_STABILITY_FAILED"
    if (/douyin_topic_input_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_INPUT_FAILED"
    if (/douyin_topic_space_commit_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_SPACE_COMMIT_FAILED"
    if (/douyin_topic_readback_runtime_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_READBACK_RUNTIME_FAILED"
    if (/douyin_topic_preflight_readback_runtime_failed/i.test(message)) return "EGO_DOUYIN_TOPIC_PREFLIGHT_READBACK_RUNTIME_FAILED"
    if (/douyin_topic_preflight_readback_invalid/i.test(message)) return "EGO_DOUYIN_TOPIC_PREFLIGHT_READBACK_INVALID"
    if (/douyin_topic_preflight_topics_present/i.test(message)) return "EGO_DOUYIN_TOPIC_PREFLIGHT_TOPICS_PRESENT"
    if (/douyin_topic_preflight_body_mismatch/i.test(message)) return "EGO_DOUYIN_TOPIC_PREFLIGHT_BODY_MISMATCH"
    if (/douyin_body_readback_runtime_failed/i.test(message)) return "EGO_DOUYIN_BODY_READBACK_RUNTIME_FAILED"
    if (/douyin_topic_readback_invalid/i.test(message)) return "EGO_DOUYIN_TOPIC_READBACK_INVALID"
    if (/douyin_topic_readback_empty/i.test(message)) return "EGO_DOUYIN_TOPIC_READBACK_EMPTY"
    if (/douyin_topic_readback_count_mismatch/i.test(message)) return "EGO_DOUYIN_TOPIC_READBACK_COUNT_MISMATCH"
    if (/douyin_topic_readback_value_mismatch/i.test(message)) return "EGO_DOUYIN_TOPIC_READBACK_VALUE_MISMATCH"
    if (/douyin_topic_/i.test(message)) return "EGO_DOUYIN_TOPIC_READBACK_MISMATCH"
    if (/douyin_schedule_mode_selector_missing_or_ambiguous/i.test(message)) return "EGO_DOUYIN_SCHEDULE_MODE_CONTROL_MISSING"
    if (/douyin_schedule_mode_readback_mismatch/i.test(message)) return "EGO_DOUYIN_SCHEDULE_MODE_READBACK_MISMATCH"
    if (/douyin_schedule_time_selector_missing_or_ambiguous/i.test(message)) return "EGO_DOUYIN_SCHEDULE_TIME_CONTROL_MISSING"
    if (/douyin_schedule_time_readback_mismatch/i.test(message)) return "EGO_DOUYIN_SCHEDULE_TIME_READBACK_MISMATCH"
    if (/douyin_schedule_immediate_readback_mismatch/i.test(message)) return "EGO_DOUYIN_IMMEDIATE_MODE_READBACK_MISMATCH"
    if (/douyin_schedule_time_invalid/i.test(message)) return "EGO_DOUYIN_SCHEDULE_TIME_INVALID"
    if (/douyin_final_button_missing_or_ambiguous|final_review_missing/i.test(message)) return "EGO_DOUYIN_FINAL_CONTROL_MISSING"
  }
  if (platformCode === "youtube") {
    if (/youtube_schedule_mode_missing_or_ambiguous/i.test(message)) return "EGO_YOUTUBE_SCHEDULE_MODE_CONTROL_MISSING"
    if (/youtube_schedule_mode_readback_mismatch/i.test(message)) return "EGO_YOUTUBE_SCHEDULE_MODE_READBACK_MISMATCH"
    if (/youtube_schedule_date_control_missing_or_ambiguous/i.test(message)) return "EGO_YOUTUBE_SCHEDULE_DATE_CONTROL_MISSING"
    if (/youtube_schedule_date_option_missing_or_ambiguous/i.test(message)) return "EGO_YOUTUBE_SCHEDULE_DATE_OPTION_MISSING"
    if (/youtube_schedule_date_readback_mismatch/i.test(message)) return "EGO_YOUTUBE_SCHEDULE_DATE_READBACK_MISMATCH"
    if (/youtube_schedule_time_control_missing_or_ambiguous/i.test(message)) return "EGO_YOUTUBE_SCHEDULE_TIME_CONTROL_MISSING"
    if (/youtube_schedule_time_readback_mismatch/i.test(message)) return "EGO_YOUTUBE_SCHEDULE_TIME_READBACK_MISMATCH"
    if (/youtube_schedule_final_button_missing_or_ambiguous/i.test(message)) return "EGO_YOUTUBE_SCHEDULE_FINAL_CONTROL_MISSING"
    if (/youtube_schedule_time_invalid/i.test(message)) return "EGO_YOUTUBE_SCHEDULE_TIME_INVALID"
  }
  if (platformCode === "bilibili") {
    if (/bilibili_content_type_unsupported/i.test(message)) return "EGO_BILIBILI_CONTENT_TYPE_UNSUPPORTED"
    if (/bilibili_video_media_invalid/i.test(message)) return "EGO_BILIBILI_MEDIA_INVALID"
    if (/bilibili_foreign_draft/i.test(message)) return "EGO_BILIBILI_FOREIGN_DRAFT"
    if (/bilibili_video_input_missing/i.test(message)) return "EGO_BILIBILI_VIDEO_INPUT_MISSING"
    if (/bilibili_upload_failed/i.test(message)) return "EGO_BILIBILI_UPLOAD_FAILED"
    if (/bilibili_upload_timeout/i.test(message)) return "EGO_BILIBILI_UPLOAD_TIMEOUT"
    if (/bilibili_title_input_missing/i.test(message)) return "EGO_BILIBILI_TITLE_INPUT_MISSING"
    if (/bilibili_description_fill_failed/i.test(message)) return "EGO_BILIBILI_DESCRIPTION_FILL_FAILED"
    if (/bilibili_content_readback_mismatch/i.test(message)) return "EGO_BILIBILI_CONTENT_READBACK_MISMATCH"
    if (/bilibili_final_button_missing_or_disabled/i.test(message)) return "EGO_BILIBILI_FINAL_CONTROL_MISSING"
  }
  if (platformCode === "kuaishou") {
    if (/kuaishou_schedule_mode_missing_or_ambiguous/i.test(message)) return "EGO_KUAISHOU_SCHEDULE_MODE_CONTROL_MISSING"
    if (/kuaishou_schedule_mode_readback_mismatch/i.test(message)) return "EGO_KUAISHOU_SCHEDULE_MODE_READBACK_MISMATCH"
    if (/kuaishou_schedule_picker_missing_or_ambiguous/i.test(message)) return "EGO_KUAISHOU_SCHEDULE_PICKER_MISSING"
    if (/kuaishou_schedule_date_missing_or_ambiguous/i.test(message)) return "EGO_KUAISHOU_SCHEDULE_DATE_CONTROL_MISSING"
    if (/kuaishou_schedule_time_missing_or_ambiguous/i.test(message)) return "EGO_KUAISHOU_SCHEDULE_TIME_CONTROL_MISSING"
    if (/kuaishou_schedule_time_readback_mismatch/i.test(message)) return "EGO_KUAISHOU_SCHEDULE_TIME_READBACK_MISMATCH"
    if (/kuaishou_schedule_final_button_missing_or_disabled/i.test(message)) return "EGO_KUAISHOU_FINAL_CONTROL_MISSING"
    if (/kuaishou_schedule_time_invalid/i.test(message)) return "EGO_KUAISHOU_SCHEDULE_TIME_INVALID"
  }
  return "EGO_PUBLISH_PREPARE_REVIEW_REQUIRED"
}
for (const target of publishTargets) {
  let status = "failed"
  let errorCode = "EGO_PREPARE_ADAPTER_UNVERIFIED"
  let prepareStage = "composer_open"
  let diagnostic
  let douyinDiagnosticBodySelector = ""
	  douyinTopicContext.branch = ""
	  douyinTopicContext.stage = ""
	  douyinTopicContext.topics = []
  if (remainingPublishBudget() < 1200) {
    preparationResults.push({ platformCode: target.platformCode, status, errorCode: "EGO_PUBLISH_PREPARE_TIMEOUT" })
    continue
  }
  try {
    if (target.prepareMode === "manual_handoff") {
      preparationResults.push({ platformCode: target.platformCode, status, errorCode: "EGO_PUBLISH_AUTOMATION_UNSUPPORTED" })
      continue
    }
    await alphaInteractionPause(target.preparePauseMs, 450, 800)
    const douyinGraphText = target.platformCode === "douyin" && target.contentType === "graph_text"
    const composerURL = target.platformCode === "kuaishou" && target.contentType === "graph_text" && !target.composerURL.includes("?")
      ? target.composerURL + "?tabType=3"
      : target.composerURL
    const entryURL = douyinGraphText ? "https://creator.douyin.com/creator-micro/home" : composerURL
    const tabsBeforeComposer = await listTabs()
    const reusableComposerTab = tabsBeforeComposer.find((tab) => {
      try {
        const current = new URL(String(tab?.url || ""))
        const expected = new URL(entryURL)
        return current.origin === expected.origin && current.pathname === expected.pathname && current.search === expected.search
      } catch {
        return false
      }
    })
    if (reusableComposerTab) {
      await switchTab(reusableComposerTab.targetId ?? reusableComposerTab.id)
    } else {
      if (tabsBeforeComposer.length >= 3) throw new Error("publish_composer_tab_limit")
      await openOrReuseTab(entryURL, { wait: true, timeout: 8 })
    }
    await new Promise((resolve) => setTimeout(resolve, 400))
    const page = await pageInfo()
    const pageURL = String(page?.url || "")
    if (/\/login|passport|signin/i.test(pageURL)) {
      errorCode = "LOGIN_REQUIRED"
    } else if (await alphaPlatformChallengeVisible()) {
      errorCode = "LOGIN_REQUIRED"
          } else if (target.platformCode === "youtube" || target.platformCode === "tiktok") {
      const youtubeDeadlineAt = Date.now() + 90000
      const youtubeRemaining = () => youtubeDeadlineAt - Date.now()
      const clearYouTubeMarkers = async () => js("(() => {" +
        "for (const node of document.querySelectorAll('*')) for (const name of node.getAttributeNames()) " +
        "if (name.startsWith('data-alpha-youtube-')) node.removeAttribute(name); return true;})()")
      const exactYouTubeControl = async (labels, marker) => {
        await clearYouTubeMarkers()
        return js("(() => {" +
          "const labels=" + JSON.stringify(labels) + ";" +
          "const marker=" + JSON.stringify(marker) + ";" +
          "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const enabled=(node)=>!node.disabled&&node.getAttribute('aria-disabled')!=='true';" +
          "const controls=[...document.querySelectorAll('button,[role=\\\"button\\\"],tp-yt-paper-button,tp-yt-paper-radio-button,tp-yt-paper-item,[role=\\\"radio\\\"],[role=\\\"option\\\"]')].filter((node)=>visible(node)&&enabled(node)&&labels.includes((node.innerText||node.textContent||node.getAttribute('aria-label')||'').trim()));" +
          "if(controls.length!==1)return '';controls[0].setAttribute('data-alpha-youtube-'+marker,'true');return '[data-alpha-youtube-'+marker+'=\\\"true\\\"]';})()")
      }
      const uniqueYouTubeSelector = async (selectors, marker) => {
        await clearYouTubeMarkers()
        return js("(() => {" +
          "const selectors=" + JSON.stringify(selectors) + ";const marker=" + JSON.stringify(marker) + ";" +
          "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const nodes=[];for(const selector of selectors)for(const node of document.querySelectorAll(selector))if(visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true')nodes.push(node);" +
          "const unique=[...new Set(nodes)];if(unique.length!==1)return '';unique[0].setAttribute('data-alpha-youtube-'+marker,'true');return '[data-alpha-youtube-'+marker+'=\\\"true\\\"]';})()")
      }
      const selectedYouTubeControl = async (selector) => js("(() => {" +
        "const node=document.querySelector(" + JSON.stringify(selector) + ");" +
        "return Boolean(node&&(node.getAttribute('aria-checked')==='true'||node.getAttribute('aria-selected')==='true'||node.hasAttribute('checked')||node.hasAttribute('selected')||node.classList.contains('iron-selected')||node.querySelector('[aria-checked=\\\"true\\\"],[aria-selected=\\\"true\\\"],[checked],[selected]')));})()")
      const uniqueYouTubeText = async (selectors, accepted) => js("(() => {" +
        "const selectors=" + JSON.stringify(selectors) + ";const accepted=" + JSON.stringify(accepted) + ";" +
        "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const nodes=[];for(const selector of selectors)for(const node of document.querySelectorAll(selector))if(visible(node))nodes.push(node);" +
        "const unique=[...new Set(nodes)];if(unique.length!==1)return '';const text=(unique[0].innerText||unique[0].textContent||'').trim();return accepted.includes(text)?text:'';})()")
      const youtubePreviewSignature = async () => js("(() => {" +
        "const selectors=['ytcp-video-metadata-editor-thumb img[src]','#thumbnail-editor img[src]','ytcp-video-metadata-editor-thumb #thumbnail-container','ytcp-video-metadata-editor-thumb [style*=\\\"background-image\\\"]'];" +
        "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const sources=[];for(const selector of selectors)for(const node of document.querySelectorAll(selector)){if(!visible(node))continue;const background=node.style.backgroundImage||getComputedStyle(node).backgroundImage||'';const source=node.currentSrc||node.src||background;if(source&&source!=='none')sources.push(String(source));}return [...new Set(sources)].join('|');})()")
      if (target.contentType !== "video") throw new Error("EGO_YOUTUBE_CONTENT_TYPE_UNSUPPORTED")
      const videoMedia = target.media.filter((item) => item.role === "video")
      const coverMedia = target.media.filter((item) => item.role === "cover")
      if (videoMedia.length !== 1 || coverMedia.length > 1 || videoMedia.length + coverMedia.length !== target.media.length) {
        throw new Error("EGO_YOUTUBE_MEDIA_INVALID")
      }

      const uploadEntrySelector = await exactYouTubeControl(["Upload videos", "上传视频"], "upload")
      if (!uploadEntrySelector) throw new Error("youtube_upload_entry_missing")
      await click(uploadEntrySelector, { label: "Upload videos" })
      await new Promise((resolve) => setTimeout(resolve, 500))
      const videoInputSelector = await js("(() => {" +
        "const inputs=[...document.querySelectorAll(\"input[type='file']\")];" +
        "const candidates=inputs.filter((node)=>/video/i.test(String(node.accept||'')));" +
        "if(candidates.length!==1)return '';candidates[0].setAttribute('data-alpha-youtube-video-input','true');return '[data-alpha-youtube-video-input=\\\"true\\\"]';})()")
      if (!videoInputSelector) throw new Error("youtube_video_input_missing")
      await uploadFile(videoInputSelector, videoMedia[0].filePath)
      try { await waitForNetworkIdle({ timeout: 8 }) } catch {}
      let uploadFields = null
      for (let attempt = 0; attempt < 180 && youtubeRemaining() > 1200; attempt += 1) {
        uploadFields = await js("(() => {" +
          "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const title=[\"#title-textarea #textbox\",\"ytcp-social-suggestions-textbox[id='title-textarea'] #textbox\"].find((selector)=>visible(document.querySelector(selector)))||'';" +
          "const body=[\"#description-textarea #textbox\",\"ytcp-social-suggestions-textbox[id='description-textarea'] #textbox\"].find((selector)=>visible(document.querySelector(selector)))||'';" +
          "const dialog=document.querySelector('.dialog-content.style-scope.ytcp-uploads-dialog');" +
          "const text=(document.body?.innerText||'').slice(0,12000);" +
          "const input=document.querySelector('[data-alpha-youtube-video-input=\\\"true\\\"]');" +
          "const uploadError=/上传失败|上传错误|upload failed|upload error|failed to upload/i.test(text);" +
          "const uploading=/上传中|正在上传|处理中|转码中|uploading|processing/i.test(text);" +
          "return {dialog:Boolean(dialog)&&visible(dialog),title,body,fileSelected:Boolean(input?.files?.length),uploadError,uploading};})()")
        if (uploadFields?.dialog && uploadFields?.title && uploadFields?.fileSelected && !uploadFields.uploadError && !uploadFields.uploading) break
        await new Promise((resolve) => setTimeout(resolve, 500))
      }
      if (!uploadFields?.dialog || !uploadFields?.title || !uploadFields?.fileSelected || uploadFields.uploadError || uploadFields.uploading) {
        throw new Error("EGO_YOUTUBE_UPLOAD_TIMEOUT")
      }

      await fillInput(uploadFields.title, target.title)
      if (target.body) {
        if (!uploadFields.body) throw new Error("youtube_body_missing")
        if (uploadFields.body.includes("#textbox")) {
          if (!await fillRichEditor(uploadFields.body, target.body)) throw new Error("youtube_body_fill_failed")
        } else {
          await fillInput(uploadFields.body, target.body)
        }
      }
      const contentReadback = await js("(() => {" +
        "const expectedTitle=" + JSON.stringify(target.title) + ",expectedBody=" + JSON.stringify(target.body) + ";" +
        "const titleNode=document.querySelector(\"#title-textarea #textbox,ytcp-social-suggestions-textbox[id='title-textarea'] #textbox\");" +
        "const bodyNode=document.querySelector(\"#description-textarea #textbox,ytcp-social-suggestions-textbox[id='description-textarea'] #textbox\");" +
        "const actualTitle=titleNode?.value||titleNode?.innerText||titleNode?.textContent||'';" +
        "const actualBody=(bodyNode?.value||bodyNode?.innerText||bodyNode?.textContent||'').replace(/\\r\\n?/g,'\\n');" +
        "return actualTitle===expectedTitle&&(expectedBody===''||actualBody===expectedBody||actualBody===expectedBody+'\\n');})()")
      if (!contentReadback) throw new Error("youtube_content_readback_mismatch")

      if (coverMedia.length === 1) {
        const beforeCover = await youtubePreviewSignature()
        const coverTriggerSelector = await exactYouTubeControl(["Upload thumbnail", "上传缩略图"], "cover-trigger")
        if (!coverTriggerSelector) throw new Error("youtube_cover_trigger_missing")
        await click(coverTriggerSelector, { label: "Upload thumbnail" })
        await new Promise((resolve) => setTimeout(resolve, 300))
        const coverInputSelector = await js("(() => {" +
          "const inputs=[...document.querySelectorAll(\"input[type='file']\")];" +
          "const candidates=inputs.filter((node)=>/image|png|jpe?g|webp/i.test(String(node.accept||'')));" +
          "if(candidates.length!==1)return '';candidates[0].setAttribute('data-alpha-youtube-cover-input','true');return '[data-alpha-youtube-cover-input=\\\"true\\\"]';})()")
        if (!coverInputSelector) throw new Error("youtube_cover_input_missing")
        await uploadFile(coverInputSelector, coverMedia[0].filePath)
        const coverDeadline = Date.now() + 15000
        let afterCover = ""
        while (Date.now() < coverDeadline) {
          afterCover = await youtubePreviewSignature()
          if (afterCover && afterCover !== beforeCover) break
          await new Promise((resolve) => setTimeout(resolve, 300))
        }
        if (!afterCover || afterCover === beforeCover) throw new Error("youtube_cover_preview_missing")
      }

      const showMoreSelector = await exactYouTubeControl(["Show more", "显示更多"], "show-more")
      if (!showMoreSelector) throw new Error("youtube_show_more_missing")
      await click(showMoreSelector, { label: "Show more" })
      await new Promise((resolve) => setTimeout(resolve, 300))

      if (target.tags.length) {
        const tagsInputSelector = await uniqueYouTubeSelector(["input[aria-label='Tags']", "input[aria-label='标签']", "#tags-container input"], "tags-input")
        if (!tagsInputSelector) throw new Error("youtube_tags_input_missing")
        for (const tag of target.tags) {
          await fillInput(tagsInputSelector, tag)
          await pressKey("ENTER")
          await new Promise((resolve) => setTimeout(resolve, 180))
        }
        const tagsReadback = await js("(() => {" +
          "const expected=" + JSON.stringify(target.tags) + ";" +
          "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const chips=[...document.querySelectorAll(\"ytcp-chip,[role='listitem'].chip,.chip[aria-label*='edit' i],.chip[aria-label*='编辑']\")].filter((node)=>visible(node)&&(node.matches('ytcp-chip')||node.querySelector(\"button,[aria-label*='Remove' i],[aria-label*='删除']\")||/press enter to edit|按 enter.*编辑/i.test(node.getAttribute('aria-label')||'')));" +
          "const values=chips.map((chip)=>{const text=chip.querySelector('#chip-text,.text')?.textContent||chip.getAttribute('aria-label')?.replace(/\\.?\\s*press enter to edit.*$/i,'')||chip.innerText||chip.textContent||'';return text.trim()}).filter(Boolean);" +
          "return values.length===expected.length&&new Set(values).size===expected.length&&expected.every((tag)=>values.includes(tag));})()")
        if (!tagsReadback) throw new Error("youtube_tags_readback_mismatch")
      }

      const audienceLabels = target.audience === "not_made_for_kids"
        ? ["No, it's not made for kids", "否，它不是面向儿童的内容"]
        : target.audience === "made_for_kids"
          ? ["Yes, it's made for kids", "是，它是面向儿童的内容"]
          : []
      if (!audienceLabels.length) throw new Error("youtube_audience_unsupported")
      const audienceSelector = await exactYouTubeControl(audienceLabels, "audience")
      if (!audienceSelector) throw new Error("youtube_audience_missing")
      await click(audienceSelector, { label: "select YouTube audience" })
      if (!await selectedYouTubeControl(audienceSelector)) throw new Error("youtube_audience_readback_mismatch")

      const playlistSelectors = ["ytcp-video-metadata-playlists ytcp-dropdown-trigger", "ytcp-video-metadata-playlists #dropdown-trigger", "#playlists-container #dropdown-trigger"]
      if (!target.playlist) {
        const playlistValue = await uniqueYouTubeText(playlistSelectors, ["Select", "选择"])
        if (!playlistValue) throw new Error("youtube_playlist_empty_readback_mismatch")
      } else {
        const playlistTriggerSelector = await uniqueYouTubeSelector(playlistSelectors, "playlist-trigger")
        if (!playlistTriggerSelector) throw new Error("youtube_playlist_trigger_missing")
        await click(playlistTriggerSelector, { label: "open YouTube playlist" })
        const playlistOptionSelector = await exactYouTubeControl([target.playlist], "playlist-option")
        if (!playlistOptionSelector) throw new Error("youtube_playlist_option_missing")
        await click(playlistOptionSelector, { label: "select YouTube playlist" })
        const playlistValue = await uniqueYouTubeText(playlistSelectors, [target.playlist])
        if (playlistValue !== target.playlist) throw new Error("youtube_playlist_readback_mismatch")
      }

      const commentsExpected = target.allow_comments ? ["Comments On", "评论 开启"] : ["Comments Off", "评论 关闭"]
      const commentsSelector = await uniqueYouTubeSelector(["#enablement-state-select ytcp-dropdown-trigger"], "comments-trigger")
      if (!commentsSelector) throw new Error("youtube_comments_trigger_missing")
      let commentsValue = await uniqueYouTubeText(["#enablement-state-select ytcp-dropdown-trigger"], commentsExpected)
      if (!commentsValue) {
        await click(commentsSelector, { label: "open YouTube comments setting" })
        const commentOptionSelector = await exactYouTubeControl(target.allow_comments ? ["On", "开启"] : ["Off", "关闭"], "comments-option")
        if (!commentOptionSelector) throw new Error("youtube_comments_option_missing")
        await click(commentOptionSelector, { label: "select YouTube comments setting" })
        commentsValue = await uniqueYouTubeText(["#enablement-state-select ytcp-dropdown-trigger"], commentsExpected)
      }
      if (!commentsValue) throw new Error("youtube_comments_readback_mismatch")

      const alteredLabel = target.altered_content ? "Yes, AI was used" : "No, AI wasn’t used"
      const alteredSelector = await uniqueYouTubeSelector(["[role='radio'][aria-label=" + JSON.stringify(alteredLabel) + "]"], "altered-content")
      if (!alteredSelector) throw new Error("youtube_altered_content_unsupported")
      await click(alteredSelector, { label: "select YouTube altered content" })
      if (!await selectedYouTubeControl(alteredSelector)) throw new Error("youtube_altered_content_readback_mismatch")

      for (let step = 0; step < 3; step += 1) {
        const nextSelector = await exactYouTubeControl(["Next", "下一步"], "next-" + step)
        if (!nextSelector) throw new Error("youtube_next_missing")
        await click(nextSelector, { label: "advance YouTube upload details" })
        try { await waitForNetworkIdle({ timeout: 8 }) } catch {}
        await new Promise((resolve) => setTimeout(resolve, 500))
      }

      for (let dialogStep = 0; dialogStep < 3; dialogStep += 1) {
        const dialogDismissSelector = await js("(() => {" +
          "for(const node of document.querySelectorAll('[data-alpha-youtube-dialog-dismiss]'))node.removeAttribute('data-alpha-youtube-dialog-dismiss');" +
          "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const dialogs=[...document.querySelectorAll('[role=\\\"dialog\\\"],tp-yt-paper-dialog,ytcp-dialog')].filter(visible);" +
          "if(!dialogs.length)return '';if(dialogs.length!==1)return '__ambiguous__';" +
          "const labels=['Next','下一步','Got it','知道了','Done','完成'];" +
          "const controls=[...dialogs[0].querySelectorAll('button,[role=\\\"button\\\"],tp-yt-paper-button')].filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&labels.includes((node.innerText||node.textContent||'').trim()));" +
          "if(controls.length!==1)return '__ambiguous__';controls[0].setAttribute('data-alpha-youtube-dialog-dismiss','true');return '[data-alpha-youtube-dialog-dismiss=\\\"true\\\"]';})()")
        if (!dialogDismissSelector) break
        if (dialogDismissSelector === "__ambiguous__") throw new Error("youtube_post_advance_dialog_ambiguous")
        await click(dialogDismissSelector, { label: "dismiss YouTube review dialog" })
        await new Promise((resolve) => setTimeout(resolve, 300))
      }

      const normalizeYouTubeScheduleText = (value) => String(value || '').normalize('NFKC').replace(/\s+/g, ' ').trim()
      const youtubeExpectedSchedule = (publishAt) => {
        const instant = new Date(publishAt)
        if (Number.isNaN(instant.getTime()) || instant.getUTCSeconds() !== 0 || instant.getUTCMilliseconds() !== 0) return null
        const fields = Object.fromEntries(new Intl.DateTimeFormat('en-GB', {
          timeZone: 'Asia/Shanghai', calendar: 'gregory', year: 'numeric', month: '2-digit', day: '2-digit',
          hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
        }).formatToParts(instant).filter((part) => part.type !== 'literal').map((part) => [part.type, part.value]))
        const year = Number(fields.year), month = Number(fields.month), day = Number(fields.day)
        const hour = Number(fields.hour), minute = Number(fields.minute)
        if (![year, month, day, hour, minute].every(Number.isInteger) || month < 1 || month > 12 || day < 1 || day > 31 || hour < 0 || hour > 23 || minute < 0 || minute > 59) return null
        const pad = (value) => String(value).padStart(2, '0')
        const date = year + '-' + pad(month) + '-' + pad(day)
        const time = pad(hour) + ':' + pad(minute)
        const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
        const period = hour >= 12 ? 'PM' : 'AM'
        const twelveHour = hour %% 12 || 12
        const timeLabel = twelveHour + ':' + pad(minute) + '\u202f' + period
        return {
          date,
          time,
          dateTimeMinute: date + ' ' + time,
          dateLabels: [months[month - 1] + ' ' + day + ', ' + year, year + '年' + month + '月' + day + '日'],
          timeLabels: [timeLabel, timeLabel.replace('\u202f', ' ')],
        }
      }
      const uniqueYouTubeScheduleOption = async (marker) => js("(() => {" +
        "const marker=" + JSON.stringify(marker) + ";const attribute='data-alpha-youtube-'+marker;" +
        "for(const node of document.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);" +
        "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const normalize=(value)=>String(value||'').normalize('NFKC').replace(/\\s+/g,' ').trim();" +
        "const wanted=new Set(['Schedule','定时发布']);const roots=[...document.querySelectorAll('ytcp-visibility-scheduler')].filter(visible);" +
        "if(roots.length!==1)return '';const raw=[...roots[0].querySelectorAll(\"tp-yt-paper-radio-button,[role='radio'],label\")].filter(visible).filter((node)=>wanted.has(normalize(node.innerText||node.textContent||node.getAttribute('aria-label')||'')));" +
        "const controls=raw.filter((node)=>!raw.some((other)=>other!==node&&other.contains(node)));if(controls.length!==1)return '';controls[0].setAttribute(attribute,'true');return '['+attribute+'=\\\"true\\\"]';})()")
      const youtubeScheduleSelected = async () => js("(() => {" +
        "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const normalize=(value)=>String(value||'').normalize('NFKC').replace(/\\s+/g,' ').trim();const wanted=new Set(['Schedule','定时发布']);" +
        "const roots=[...document.querySelectorAll('ytcp-visibility-scheduler')].filter(visible);if(roots.length!==1)return false;" +
        "const raw=[...roots[0].querySelectorAll(\"tp-yt-paper-radio-button,[role='radio'],label\")].filter(visible).filter((node)=>wanted.has(normalize(node.innerText||node.textContent||node.getAttribute('aria-label')||'')));" +
        "const controls=raw.filter((node)=>!raw.some((other)=>other!==node&&other.contains(node)));if(controls.length!==1)return false;" +
        "const state=(node)=>{if(node.checked===true)return true;if(node.checked===false)return false;for(const name of ['aria-checked','aria-selected','data-selected']){const value=node.getAttribute(name);if(value==='true')return true;if(value==='false')return false}if(node.hasAttribute('checked')||node.hasAttribute('selected')||node.classList.contains('iron-selected')||node.classList.contains('selected')||node.classList.contains('active'))return true;const nested=[...node.querySelectorAll(\"input[type='radio'],input[type='checkbox'],[role='radio'],[role='checkbox'],[aria-checked]\")];return nested.length===1?state(nested[0]):false};return state(controls[0]);})()")
      const uniqueYouTubeScheduleDateOption = async (expected, marker) => js("(() => {" +
        "const expected=" + JSON.stringify(expected) + ",marker=" + JSON.stringify(marker) + ";const attribute='data-alpha-youtube-'+marker;" +
        "for(const node of document.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);" +
        "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const normalize=(value)=>String(value||'').normalize('NFKC').replace(/\\s+/g,' ').trim();const wanted=new Set(expected.map(normalize));" +
        "const raw=[...document.querySelectorAll(\"tp-yt-paper-item,[role='option'],[role='gridcell'],button,li\")].filter(visible).filter((node)=>wanted.has(normalize(node.innerText||node.textContent||'')));" +
        "const controls=raw.filter((node)=>!raw.some((other)=>other!==node&&other.contains(node)));if(controls.length!==1)return '';controls[0].setAttribute(attribute,'true');return '['+attribute+'=\\\"true\\\"]';})()")
      const youtubeScheduleReadbackStable = async (dateSelector, timeSelector, expected) => {
        let previous = ''
        for (let attempt = 0; attempt < 2; attempt += 1) {
          const readback = await js("(() => {const date=document.querySelector(" + JSON.stringify(dateSelector) + "),time=document.querySelector(" + JSON.stringify(timeSelector) + ");return {date:date?.innerText||date?.textContent||'',time:time?.value||time?.getAttribute('value')||''}})()")
          const dateText = normalizeYouTubeScheduleText(readback?.date)
          const timeText = normalizeYouTubeScheduleText(readback?.time)
          const dateMatches = expected.dateLabels.some((label) => normalizeYouTubeScheduleText(label) === dateText)
          const timeMatches = expected.timeLabels.some((label) => normalizeYouTubeScheduleText(label) === timeText)
          if (dateMatches && timeMatches && await youtubeScheduleSelected()) {
            const signature = dateText + '|' + timeText
            if (signature === previous) return true
            previous = signature
          }
          await new Promise((resolve) => setTimeout(resolve, 350))
        }
        return false
      }
      let finalSelector = ""
      if (target.publishAt) {
        const expectedSchedule = youtubeExpectedSchedule(target.publishAt)
        if (!expectedSchedule || new Date(target.publishAt).getTime() <= Date.now()) throw new Error("youtube_schedule_time_invalid")
        prepareStage = "schedule_mode"
        const scheduleSelector = await uniqueYouTubeScheduleOption("schedule-mode")
        if (!scheduleSelector) throw new Error("youtube_schedule_mode_missing_or_ambiguous")
        if (!await youtubeScheduleSelected()) {
          await click(scheduleSelector, { label: "select YouTube Schedule" })
          await new Promise((resolve) => setTimeout(resolve, 300))
        }
        if (!await youtubeScheduleSelected()) throw new Error("youtube_schedule_mode_readback_mismatch")
        prepareStage = "schedule_date"
        const dateSelector = await uniqueYouTubeSelector(['ytcp-datetime-picker ytcp-text-dropdown-trigger'], "schedule-date")
        const timeSelector = await uniqueYouTubeSelector(['input#time-of-day', '#time-of-day input', 'ytcp-datetime-picker form input', 'ytcp-datetime-picker tp-yt-paper-input input'], "schedule-time")
        if (!dateSelector) throw new Error("youtube_schedule_date_control_missing_or_ambiguous")
        if (!timeSelector) throw new Error("youtube_schedule_time_control_missing_or_ambiguous")
        const currentDate = normalizeYouTubeScheduleText(await js("(() => document.querySelector(" + JSON.stringify(dateSelector) + ")?.innerText || document.querySelector(" + JSON.stringify(dateSelector) + ")?.textContent || '')()"))
        if (!expectedSchedule.dateLabels.some((label) => normalizeYouTubeScheduleText(label) === currentDate)) {
          await click(dateSelector, { label: "open YouTube schedule date" })
          await new Promise((resolve) => setTimeout(resolve, 150))
          const optionSelector = await uniqueYouTubeScheduleDateOption(expectedSchedule.dateLabels, "schedule-date-option")
          if (!optionSelector) throw new Error("youtube_schedule_date_option_missing_or_ambiguous")
          await click(optionSelector, { label: "select YouTube schedule date" })
          await new Promise((resolve) => setTimeout(resolve, 250))
        }
        prepareStage = "schedule_time"
        let timeReadback = false
        for (const value of expectedSchedule.timeLabels) {
          await fillInput(timeSelector, value)
          await pressKey("TAB")
          await new Promise((resolve) => setTimeout(resolve, 250))
          if (await youtubeScheduleReadbackStable(dateSelector, timeSelector, expectedSchedule)) {
            timeReadback = true
            break
          }
        }
        if (!timeReadback) throw new Error("youtube_schedule_time_readback_mismatch")
        prepareStage = "final_control"
        finalSelector = await js("(() => {" +
          "for(const node of document.querySelectorAll('[data-alpha-youtube-final-schedule]'))node.removeAttribute('data-alpha-youtube-final-schedule');" +
          "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const labels=new Set(['Schedule','定时发布']);const buttons=[...document.querySelectorAll(\"button,[role='button'],tp-yt-paper-button\")].filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&labels.has((node.innerText||node.textContent||'').trim()));" +
          "if(buttons.length!==1)return '';buttons[0].setAttribute('data-alpha-youtube-final-schedule','true');return '[data-alpha-youtube-final-schedule=\\\"true\\\"]';})()")
        if (!finalSelector) throw new Error("youtube_schedule_final_button_missing_or_ambiguous")
      } else {
        const visibilityLabels = target.visibility === "private"
          ? ["Private", "私享"]
          : target.visibility === "unlisted"
            ? ["Unlisted", "不公开列出"]
            : target.visibility === "public"
              ? ["Public", "公开"]
              : []
        if (!visibilityLabels.length) throw new Error("youtube_visibility_unsupported")
        const visibilitySelector = await exactYouTubeControl(visibilityLabels, "visibility")
        if (!visibilitySelector) throw new Error("youtube_visibility_missing")
        await click(visibilitySelector, { label: "select YouTube visibility" })
        if (!await selectedYouTubeControl(visibilitySelector)) throw new Error("youtube_visibility_readback_mismatch")
        while (youtubeRemaining() > 1200 && !finalSelector) {
          finalSelector = await exactYouTubeControl(["Save", "保存", "Publish", "发布", "Schedule", "定时发布"], "final")
          if (!finalSelector) await new Promise((resolve) => setTimeout(resolve, 500))
        }
      }
      if (!finalSelector) throw new Error("youtube_final_button_missing_or_disabled")
      status = "awaiting_review"
      errorCode = ""
    } else if (target.platformCode === "bilibili") {
      // Prepare a single Bilibili video, then stop before the final 投稿
      // action. Categories, declarations, covers, and tags remain manual.
      if (target.contentType !== "video") throw new Error("bilibili_content_type_unsupported")
      if (target.media.length !== 1 || target.media[0].kind !== "video" || target.media[0].role !== "video") throw new Error("bilibili_video_media_invalid")
      const bilibiliDeadlineAt = Date.now() + 90000
      const bilibiliRemaining = () => bilibiliDeadlineAt - Date.now()
      const expectedFileName = String(target.media[0].filePath || "").split(/[\\\\/]/).pop() || ""
      const compactBilibiliText = (value) => String(value || "").replace(/\\s+/g, " ").trim()
      const bilibiliState = async () => js("(() => {" +
        "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const compact=(value)=>String(value||'').replace(/\\s+/g,' ').trim();const text=compact(document.body?.innerText||'');" +
        "const title=[...document.querySelectorAll('input')].find((node)=>String(node.placeholder||'').includes('稿件标题'));" +
        "const description=[...document.querySelectorAll('.ql-editor[contenteditable=\\\"true\\\"]')].find(visible);" +
        "const foreignDraft=/本地浏览器存在.*未提交的视频|继续编辑/.test(text)||Boolean(title&&title.value&&title.value!==" + JSON.stringify(target.title) + ");" +
        "const uploaded=/上传完成/.test(text)&&text.includes(" + JSON.stringify(expectedFileName) + ");const uploading=/上传中|转码中|上传进度|\\d{1,3}%%/.test(text)&&!/上传完成/.test(text);const failed=/上传失败|网络错误/.test(text);" +
        "return {title:compact(title?.value||''),description:compact(description?.innerText||description?.textContent||''),foreignDraft,uploaded,uploading,failed};})()")
      prepareStage = "composer"
      const initialBilibiliState = await bilibiliState()
      if (initialBilibiliState?.foreignDraft) throw new Error("bilibili_foreign_draft")
      if (initialBilibiliState?.failed) throw new Error("bilibili_upload_failed")
      prepareStage = "video_input"
      const videoInputSelector = await js("(() => {" +
        "for(const node of document.querySelectorAll('[data-alpha-bilibili-video-input]'))node.removeAttribute('data-alpha-bilibili-video-input');" +
        "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const inputs=[...document.querySelectorAll('.bcc-upload-wrapper input[type=\\\"file\\\"]')].filter((node)=>visible(node.closest('.bcc-upload-wrapper'))&&(/video/i.test(String(node.accept||''))||!String(node.accept||'').trim()));" +
        "if(inputs.length!==1)return '';inputs[0].setAttribute('data-alpha-bilibili-video-input','true');return '[data-alpha-bilibili-video-input=\\\"true\\\"]';})()")
      if (!initialBilibiliState?.uploaded) {
        if (!videoInputSelector) throw new Error("bilibili_video_input_missing")
        await uploadFile(videoInputSelector, target.media[0].filePath)
      }
      prepareStage = "upload"
      let uploadedBilibiliState = initialBilibiliState
      let stableUploads = initialBilibiliState?.uploaded ? 1 : 0
      while (bilibiliRemaining() > 1200) {
        uploadedBilibiliState = await bilibiliState()
        if (uploadedBilibiliState?.foreignDraft) throw new Error("bilibili_foreign_draft")
        if (uploadedBilibiliState?.failed) throw new Error("bilibili_upload_failed")
        if (uploadedBilibiliState?.uploaded && !uploadedBilibiliState?.uploading) {
          stableUploads += 1
          if (stableUploads >= 2) break
        } else { stableUploads = 0 }
        await new Promise((resolve) => setTimeout(resolve, 750))
      }
      if (!uploadedBilibiliState?.uploaded || uploadedBilibiliState?.uploading || stableUploads < 2) throw new Error("bilibili_upload_timeout")
      prepareStage = "title"
      const bilibiliTitleSelector = await js("(() => {const nodes=[...document.querySelectorAll('input')].filter((node)=>String(node.placeholder||'').includes('稿件标题'));if(nodes.length!==1)return '';nodes[0].setAttribute('data-alpha-bilibili-title','true');return '[data-alpha-bilibili-title=\\\"true\\\"]';})()")
      if (!bilibiliTitleSelector) throw new Error("bilibili_title_input_missing")
      await fillInput(bilibiliTitleSelector, target.title)
      prepareStage = "description"
      const bilibiliDescriptionSelector = await js("(() => {const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const nodes=[...document.querySelectorAll('.ql-editor[contenteditable=\\\"true\\\"]')].filter(visible);if(nodes.length!==1)return '';nodes[0].setAttribute('data-alpha-bilibili-description','true');return '[data-alpha-bilibili-description=\\\"true\\\"]';})()")
      if (!bilibiliDescriptionSelector || !await fillRichEditor(bilibiliDescriptionSelector, target.body || target.caption)) throw new Error("bilibili_description_fill_failed")
      const expectedDescription = compactBilibiliText(target.body || target.caption)
      let contentReadback = false
      for (let attempt = 0; attempt < 2; attempt += 1) {
        const state = await bilibiliState()
        contentReadback = state?.title === compactBilibiliText(target.title) && state?.description === expectedDescription
        if (contentReadback) break
        await new Promise((resolve) => setTimeout(resolve, 350))
      }
      if (!contentReadback) throw new Error("bilibili_content_readback_mismatch")
      prepareStage = "final_control"
      const bilibiliFinalState = await js("(() => {const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const buttons=[...document.querySelectorAll('button,[role=\\\"button\\\"],div,span')].filter((node)=>visible(node)&&(node.innerText||node.textContent||'').trim()==='立即投稿');return {count:buttons.length,enabled:buttons.length===1&&!buttons[0].disabled&&buttons[0].getAttribute('aria-disabled')!=='true'};})()")
      if (!bilibiliFinalState || bilibiliFinalState.count !== 1 || !bilibiliFinalState.enabled) throw new Error("bilibili_final_button_missing_or_disabled")
      status = "awaiting_review"
      errorCode = ""
    } else if (target.platformCode === "xhs") {
      const xhsDeadlineAt = Date.now() + 90000
      const xhsRemaining = () => xhsDeadlineAt - Date.now()
      const xhsUniqueSelector = async (selectors, marker) => js("(() => {" +
        "const selectors=" + JSON.stringify(selectors) + ",marker=" + JSON.stringify(marker) + ";" +
        "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const nodes=[];for(const selector of selectors)for(const node of document.querySelectorAll(selector))if(visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true')nodes.push(node);" +
        "const unique=[...new Set(nodes)];if(unique.length!==1)return unique.length>1?'__ambiguous__':'';" +
        "unique[0].setAttribute('data-alpha-xhs-'+marker,'true');return '[data-alpha-xhs-'+marker+'=\\\"true\\\"]';})()")
      const xhsFileSelector = async (kind, marker) => js("(() => {" +
        "const kind=" + JSON.stringify(kind) + ";" +
        "const marker=" + JSON.stringify(marker) + ";" +
        "const inputs=[...document.querySelectorAll(\"input[type='file']\")];" +
        "const candidates=inputs.filter((node)=>new RegExp(kind,'i').test(String(node.accept||'')));" +
        "if(candidates.length!==1)return candidates.length>1?'__ambiguous__':'';" +
        "candidates[0].setAttribute('data-alpha-xhs-'+marker,'true');return '[data-alpha-xhs-'+marker+'=\\\"true\\\"]';})()")
      const xhsComposerReady = await js("(() => {const url=String(location.href||'');const root=document.querySelector('.publish-page');return /creator\\.xiaohongshu\\.com\\/publish\\/publish/.test(url)&&Boolean(root||document.querySelector('#topicBtn,xhs-publish-btn'))})()")
      if (!xhsComposerReady) throw new Error("xhs_composer_mode_unverified")
      const xhsExpectedMedia = target.media.map((item) => item.filePath)
      const xhsMediaKind = target.contentType === "graph_text" ? "image" : "video"
      const xhsInputMarker = target.contentType === "graph_text" ? "image-input" : "video-input"
      const xhsInputSelector = await xhsFileSelector(xhsMediaKind, xhsInputMarker)
      if (xhsInputSelector === "__ambiguous__" || !xhsInputSelector) throw new Error("xhs_media_input_missing_or_ambiguous")
      if (target.contentType === "graph_text") {
        const xhsImageInputSelector = xhsInputSelector
        await uploadFile(xhsImageInputSelector, xhsExpectedMedia)
      } else {
        const xhsVideoInputSelector = xhsInputSelector
        await uploadFile(xhsVideoInputSelector, xhsExpectedMedia)
      }
      let xhsUploadState = null
      let xhsPreviousSignature = ""
      let xhsStableReads = 0
      while (xhsRemaining() > 1200) {
        xhsUploadState = await js("(() => {" +
          "const expected=" + JSON.stringify(xhsExpectedMedia.map((item) => item.split(/[\\\\/]/).pop())) + ";" +
          "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const input=document.querySelector(" + JSON.stringify(xhsInputSelector) + ");" +
          "const root=document.querySelector('.publish-page')||document.body;" +
          "const previews=[...new Set([...root.querySelectorAll(\"[class*='upload'] img,[class*='upload'] video,[class*='preview'] img,[class*='preview'] video,[data-testid*='preview' i] img,[data-testid*='preview' i] video\")])].filter(visible);" +
          "const files=[...(input?.files||[])].map((file)=>String(file.name||''));" +
          "const text=(document.body?.innerText||'').slice(0,12000);" +
          "const signature=previews.map((node)=>String(node.currentSrc||node.src||node.poster||'')+'|'+Math.round(node.getBoundingClientRect().width)+'x'+Math.round(node.getBoundingClientRect().height)).join('\\n');" +
          "return {selected:files.length===expected.length&&files.every((name,index)=>name===expected[index]),previewCount:previews.length,signature,uploadError:/上传失败|上传错误|upload failed|upload error/i.test(text),processing:/上传中|正在上传|处理中|转码中|uploading|processing/i.test(text)};})()")
        if (xhsUploadState?.selected && xhsUploadState.previewCount === xhsExpectedMedia.length && !xhsUploadState.uploadError && !xhsUploadState.processing) {
          if (xhsUploadState.signature && xhsUploadState.signature === xhsPreviousSignature) xhsStableReads += 1
          else xhsStableReads = 1
          xhsPreviousSignature = xhsUploadState.signature
          if (xhsStableReads >= 2) break
        } else {
          xhsStableReads = 0
          xhsPreviousSignature = ""
        }
        await new Promise((resolve) => setTimeout(resolve, 400))
      }
      if (!xhsUploadState?.selected || xhsUploadState.previewCount !== xhsExpectedMedia.length || xhsStableReads < 2 || xhsUploadState.uploadError || xhsUploadState.processing) throw new Error("xhs_upload_unverified")

      const xhsTitleSelector = await xhsUniqueSelector(["input[placeholder*='标题']", "textarea[placeholder*='标题']"], "title")
      const xhsBodySelector = await xhsUniqueSelector(["textarea[placeholder*='正文']", "div[contenteditable='true']"], "body")
      if (xhsTitleSelector === "__ambiguous__" || !xhsTitleSelector) throw new Error("xhs_title_missing_or_ambiguous")
      if (xhsBodySelector === "__ambiguous__" || !xhsBodySelector) throw new Error("xhs_body_missing_or_ambiguous")
      await fillInput(xhsTitleSelector, target.title)
      const xhsBodyValue = target.body || target.caption
      if (xhsBodySelector.includes("contenteditable")) {
        if (!await fillRichEditor(xhsBodySelector, xhsBodyValue)) throw new Error("xhs_body_fill_failed")
      } else {
        await fillInput(xhsBodySelector, xhsBodyValue)
      }
      const xhsContentReadback = await js("(() => {" +
        "const titleNode=document.querySelector(" + JSON.stringify(xhsTitleSelector) + "),bodyNode=document.querySelector(" + JSON.stringify(xhsBodySelector) + ");" +
        "const title=" + JSON.stringify(target.title) + ",body=" + JSON.stringify(xhsBodyValue) + ";" +
        "const actualTitle=titleNode?.value||titleNode?.innerText||titleNode?.textContent||'';" +
        "const actualBody=(bodyNode?.value||bodyNode?.innerText||bodyNode?.textContent||'').replace(/\\r\\n?/g,'\\n');" +
        "return actualTitle===title&&(actualBody===body||actualBody===body+'\\n');})()")
      if (!xhsContentReadback) throw new Error("xhs_content_readback_mismatch")

      for (const tag of target.tags) {
        const xhsTopicTriggerSelector = await xhsUniqueSelector(["#topicBtn"], "topic-trigger")
        if (xhsTopicTriggerSelector === "__ambiguous__" || !xhsTopicTriggerSelector) throw new Error("xhs_topic_trigger_missing_or_ambiguous")
        const beforeTopic = await js("(() => {const node=document.querySelector(" + JSON.stringify(xhsBodySelector) + ");return node?.innerText||node?.textContent||node?.value||''})()")
        await click(xhsTopicTriggerSelector, { label: "open XHS topic picker" })
        await new Promise((resolve) => setTimeout(resolve, 120))
        const topicMarker = await js("(() => {const node=document.querySelector(" + JSON.stringify(xhsBodySelector) + ");const after=node?.innerText||node?.textContent||node?.value||'';return after!==" + JSON.stringify(beforeTopic) + " && /#\\s*$/.test(after)})()")
        if (!topicMarker) throw new Error("xhs_topic_trigger_marker_missing")
        for (const character of tag) {
          await typeText(character)
          await new Promise((resolve) => setTimeout(resolve, 80))
        }
        const xhsTopicSuggestionSelector = await js("(() => {" +
          "const expected=" + JSON.stringify(tag) + ";" +
          "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const nodes=[...document.querySelectorAll(\"[role='option'],li,button,[data-topic]\")].filter((node)=>visible(node)&&!node.disabled&&((node.innerText||node.textContent||'').trim()===expected||(node.innerText||node.textContent||'').trim()==='#'+expected));" +
          "const unique=[...new Set(nodes)];if(unique.length!==1)return unique.length>1?'__ambiguous__':'';unique[0].setAttribute('data-alpha-xhs-topic-suggestion','true');return '[data-alpha-xhs-topic-suggestion=\\\"true\\\"]';})()")
        if (xhsTopicSuggestionSelector === "__ambiguous__") throw new Error("xhs_topic_suggestion_ambiguous")
        if (!xhsTopicSuggestionSelector) {
          await pressKey("SPACE")
          throw new Error("xhs_topic_suggestion_missing")
        }
        await click(xhsTopicSuggestionSelector, { label: "select exact XHS topic" })
        await new Promise((resolve) => setTimeout(resolve, 250))
      }
      const xhsTopicReadback = await js("(() => {" +
        "const expected=" + JSON.stringify(target.tags) + ";" +
        "const actual=[...document.querySelectorAll('a.tiptap-topic[data-topic]')].map((node)=>String(node.getAttribute('data-topic')||'').trim().replace(/^#/,'')).filter(Boolean);" +
        "return actual.length===expected.length&&actual.every((tag,index)=>tag===expected[index]);})()")
      if (!xhsTopicReadback) throw new Error("xhs_topic_readback_mismatch")
      const xhsBodyFinalReadback = await js("(() => {" +
        "const node=document.querySelector(" + JSON.stringify(xhsBodySelector) + ");if(!node)return false;" +
        "const clone=node.cloneNode(true);clone.querySelectorAll('a.tiptap-topic[data-topic]').forEach((topic)=>topic.remove());" +
        "const actual=(clone.innerText||clone.textContent||clone.value||'').replace(/\\r\\n?/g,'\\n');const expected=" + JSON.stringify(xhsBodyValue) + ";" +
        "return actual===expected||actual===expected+'\\n';})()")
      if (!xhsBodyFinalReadback) throw new Error("xhs_body_readback_mismatch")
      const xhsFinalSelector = await xhsUniqueSelector(["xhs-publish-btn"], "final")
      if (xhsFinalSelector === "__ambiguous__" || !xhsFinalSelector) throw new Error("xhs_final_button_missing_or_ambiguous")
      const xhsFinalEnabled = await js("(() => {const node=document.querySelector('[data-alpha-xhs-final=\\\"true\\\"]');return Boolean(node&&!node.disabled&&node.getAttribute('aria-disabled')!=='true')})()")
      if (!xhsFinalEnabled) throw new Error("xhs_final_button_missing_or_ambiguous")
      status = "awaiting_review"
      errorCode = ""
    } else if (target.platformCode === "instagram" || target.platformCode === "facebook") {
      const metaDeadlineAt = Date.now() + 90000
      const metaRemaining = () => metaDeadlineAt - Date.now()
      const metaFail = (code) => { errorCode = code; throw new Error(code) }
      const metaExpectedCaption = target.caption || target.body
      const metaPageID = String(target.metaPageID || "").trim()
      const metaPageName = String(target.metaPageName || "").trim()
      const metaInstagramID = String(target.metaInstagramID || "").trim()
      const metaInstagramName = String(target.metaInstagramName || "").trim()
      if (!/^https:\/\/business\.facebook\.com(?:\/|$)/i.test(String(target.composerURL || ""))) {
        metaFail("meta_composer_host_unverified")
      }
	  const metaComposerPath = (() => { try { return new URL(String(target.composerURL || '')).pathname.replace(/\/+$/u, '') } catch { return '' } })()
	  const metaExpectedComposerPath = target.platformCode === 'facebook'
	    ? (target.contentType === 'video' ? '/latest/reels_composer' : '/latest/composer')
	    : (target.contentType === 'video' ? '/latest/instagram_reels_composer' : '/latest/composer')
	  if (metaComposerPath !== metaExpectedComposerPath) metaFail("meta_composer_content_type_mismatch")
	  const metaMediaShapeValid = target.contentType === 'graph_text'
	    ? target.media.length > 0 && target.media.every((item) => item.kind === 'image')
	    : target.contentType === 'video' && target.media.length >= 1 && target.media.length <= 2 &&
	      target.media[0]?.kind === 'video' && target.media[0]?.role === 'video' &&
	      (target.media.length === 1 || (target.media[1]?.kind === 'image' && target.media[1]?.role === 'cover'))
	  if (!metaMediaShapeValid) metaFail("meta_media_contract_invalid")
      if (!metaPageID || !metaPageName || (target.platformCode === "instagram" && (!metaInstagramID || !metaInstagramName))) {
        metaFail("meta_target_identity_missing")
      }
      await openOrReuseTab(target.composerURL, { wait: true, timeout: 8 })
      await new Promise((resolve) => setTimeout(resolve, 500))
		const metaSurfaceReadback = await js('(() => {' +
			'const metaPageID=' + JSON.stringify(metaPageID) + ';' +
			'const searchParams=new URLSearchParams(location.search);' +
			'return location.hostname==="business.facebook.com"&&searchParams.get("asset_id")===metaPageID;' +
		'})()')
      if (!metaSurfaceReadback) metaFail("meta_page_asset_mismatch")
	  if (await alphaPlatformChallengeVisible()) {
        metaFail("PLATFORM_VERIFICATION_REQUIRED")
      }

      const metaVisible = (node) => {
        if (!node) return false
        const style = getComputedStyle(node), rect = node.getBoundingClientRect()
        return style.visibility !== "hidden" && style.display !== "none" && rect.width > 0 && rect.height > 0 && node.getAttribute("aria-hidden") !== "true"
      }
      const metaNormalize = (value) => String(value || "").replace(/[\u200B-\u200D\uFEFF]/g, "").trim().toLowerCase().replace(/\s+/g, " ")
      const metaFields = (node) => {
        if (!node) return []
        const fields = [node.innerText || node.textContent || "", node.getAttribute("aria-label") || "", node.getAttribute("title") || "", node.getAttribute("href") || ""]
        for (const name of node.getAttributeNames ? node.getAttributeNames() : []) fields.push(node.getAttribute(name) || "")
        for (const child of node.querySelectorAll ? node.querySelectorAll("*") : []) {
          if (!child.getAttributeNames) continue
          for (const name of child.getAttributeNames()) fields.push(child.getAttribute(name) || "")
        }
        return fields.map(metaNormalize).filter(Boolean)
      }
      const metaHasName = (node, expected) => {
        const name = metaNormalize(expected)
        return metaFields(node).some((value) => value === name || value.split(/\s*(?:\||·|,|，|、|\/|&|\+|和|与|及|and)\s*/).includes(name))
      }
      const metaHasID = (node, expected) => {
        const id = metaNormalize(expected)
        return metaFields(node).some((value) => value === id || value.split(/[^a-z0-9]+/i).includes(id))
      }
      const metaIdentityReadback = await js("(() => {" +
        "const metaPageID=" + JSON.stringify(metaPageID) + ",metaPageName=" + JSON.stringify(metaPageName) + ",metaInstagramID=" + JSON.stringify(metaInstagramID) + ",metaInstagramName=" + JSON.stringify(metaInstagramName) + ";" +
        "const visible=(node)=>{if(!node)return false;const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.visibility!=='hidden'&&s.display!=='none'&&r.width>0&&r.height>0&&node.getAttribute('aria-hidden')!=='true'};" +
        "const normalize=(value)=>String(value||'').replace(/[\\u200B-\\u200D\\uFEFF]/g,'').trim().toLowerCase().replace(/\\s+/g,' ');" +
        "const fields=(node)=>{if(!node)return [];const out=[node.innerText||node.textContent||'',node.getAttribute('aria-label')||'',node.getAttribute('title')||'',node.getAttribute('href')||''];for(const name of node.getAttributeNames?node.getAttributeNames():[])out.push(node.getAttribute(name)||'');for(const child of node.querySelectorAll?node.querySelectorAll('*'):[])for(const name of child.getAttributeNames?child.getAttributeNames():[])out.push(child.getAttribute(name)||'');return out.map(normalize).filter(Boolean)};" +
        "const hasName=(node,expected)=>{const name=normalize(expected);return fields(node).some((value)=>value===name||value.split(/\\s*(?:\\||·|,|，|、|\\/|&|\\+|和|与|及|and)\\s*/).includes(name))};" +
        "const hasID=(node,expected)=>{const id=normalize(expected);return fields(node).some((value)=>value===id||value.split(/[^a-z0-9]+/i).includes(id))};" +
        "const root=document.querySelector('[data-alpha-meta-composer=\"true\"]');" +
        "if(!root)return {page:false,pair:false};" +
        "const candidates=[...new Set([...root.querySelectorAll('[role=\"combobox\"],[aria-selected=\"true\"],[aria-current=\"true\"],[aria-checked=\"true\"],[data-asset-id],[data-page-id],[data-instagram-id],[data-account-id],a[href*='instagram.com']')].filter(visible))];" +
        "const currentPageID=new URLSearchParams(location.search).get(\"asset_id\")||\"\";" +
        "const pageMatches=candidates.filter((node)=>hasName(node,metaPageName));" +
        "const page=pageMatches.length===1&&(currentPageID===metaPageID||hasID(pageMatches[0],metaPageID));" +
        "let pair=false;" +
        "if(metaInstagramID&&metaInstagramName){const pairRoots=[...new Set(candidates.map((node)=>node.closest('[role=\"combobox\"]')||node).filter(visible))];const pairs=pairRoots.filter((node)=>hasName(node,metaPageName)&&hasName(node,metaInstagramName)&&hasID(node,metaInstagramID)&&(currentPageID===metaPageID||hasID(node,metaPageID)));pair=pairs.length===1;if(pair)pairs[0].setAttribute('data-alpha-meta_target_pair','true');}" +
        "if(page&&pageMatches[0])pageMatches[0].setAttribute('data-alpha-meta-page-readback','true');" +
        "return {page,pair};" +
      "})()")
      if (!metaIdentityReadback?.page) metaFail("meta_page_identity_readback_mismatch")
      if (target.platformCode === "instagram" && !metaIdentityReadback?.pair) metaFail("meta_target_pair_readback_mismatch")

      const metaComposerState = await js("(() => {" +
        "const visible=(node)=>{if(!node)return false;const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.visibility!=='hidden'&&s.display!=='none'&&r.width>0&&r.height>0&&node.getAttribute('aria-hidden')!=='true'};" +
        "const editors=[...document.querySelectorAll('[contenteditable=\"true\"],textarea,[role=\"textbox\"]')].filter(visible);" +
        "const comboboxes=[...document.querySelectorAll('[role=\"combobox\"]')].filter((node)=>visible(node)&&node.getAttribute('contenteditable')!=='true'&&!['INPUT','TEXTAREA'].includes(String(node.tagName||'').toUpperCase()));" +
        "if(editors.length!==1||comboboxes.length!==1)return {state:editors.length>1||comboboxes.length>1?'ambiguous':'missing'};" +
        "let scope=null;for(let node=comboboxes[0].parentElement;node&&node!==document.body&&node!==document.documentElement;node=node.parentElement){if(visible(node)&&node.contains(editors[0])){scope=node;break}};" +
        "if(!scope)return {state:'missing'};scope.setAttribute('data-alpha-meta-composer','true');return {state:'ready'};" +
      "})()")
      if (!metaComposerState || metaComposerState.state !== "ready") metaFail(metaComposerState?.state === "ambiguous" ? "meta_composer_ambiguous" : "meta_composer_missing")

      const metaUniqueControl = async (selectors, labels, marker) => js("(() => {" +
        "const selectors=" + JSON.stringify(selectors) + ",labels=" + JSON.stringify(labels) + ",marker=" + JSON.stringify(marker) + ";" +
        "const root=document.querySelector('[data-alpha-meta-composer=\"true\"]');if(!root)return '';" +
        "const visible=(node)=>{if(!node)return false;const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.visibility!=='hidden'&&s.display!=='none'&&r.width>0&&r.height>0&&node.getAttribute('aria-hidden')!=='true'};" +
        "const controls=[];for(const selector of selectors)for(const node of root.querySelectorAll(selector))if(visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&labels.includes((node.innerText||node.textContent||node.getAttribute('aria-label')||node.getAttribute('title')||'').trim()))controls.push(node);" +
        "const unique=[...new Set(controls)];if(unique.length!==1)return unique.length>1?'__ambiguous__':'';unique[0].setAttribute('data-alpha-meta-'+marker,'true');return '[data-alpha-meta-'+marker+'=\\\"true\\\"]';" +
      "})()")
      if (!target.media.length) metaFail("meta_media_required")
      const metaMediaTriggerSelector = await metaUniqueControl(
        ["button", "[role=\"button\"]", "label"],
        ["Add photo/video", "Add photo or video", "添加照片/视频", "添加媒体", "Add media"],
        "media-trigger",
      )
      if (metaMediaTriggerSelector === "__ambiguous__") metaFail("meta_media_trigger_ambiguous")
      if (metaMediaTriggerSelector) {
        await click(metaMediaTriggerSelector, { label: "open Meta media picker" })
        await new Promise((resolve) => setTimeout(resolve, 250))
      }
      const metaMediaInputSelector = await js("(() => {" +
        "const root=document.querySelector('[data-alpha-meta-composer=\"true\"]');if(!root)return '';const inputs=[...root.querySelectorAll(\"input[type='file']\")];if(inputs.length!==1)return inputs.length>1?'__ambiguous__':'';inputs[0].setAttribute('data-alpha-meta-media-input','true');return '[data-alpha-meta-media-input=\\\"true\\\"]';" +
      "})()")
      if (metaMediaInputSelector === "__ambiguous__") metaFail("meta_media_input_ambiguous")
      if (!metaMediaInputSelector) metaFail("meta_media_input_missing")
      const metaExpectedMedia = target.media.map((item) => item.filePath)
      await uploadFile(metaMediaInputSelector, metaExpectedMedia)
      let metaUploadState = null
      let metaPreviousSignature = ""
      let metaStableReads = 0
      while (metaRemaining() > 1200) {
        metaUploadState = await js("(() => {" +
          "const root=document.querySelector('[data-alpha-meta-composer=\"true\"]');if(!root)return null;" +
          "const visible=(node)=>{if(!node)return false;const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.visibility!=='hidden'&&s.display!=='none'&&r.width>0&&r.height>0&&node.getAttribute('aria-hidden')!=='true'};" +
          "const input=root.querySelector('[data-alpha-meta-media-input=\"true\"]'),expected=" + JSON.stringify(metaExpectedMedia.map((item) => item.split(/[\\/]/).pop())) + ";" +
          "const files=[...(input?.files||[])].map((file)=>String(file.name||''));" +
          "const previews=[...root.querySelectorAll(\"[data-testid*='preview' i] img,[data-testid*='media' i] img,[class*='preview' i] img,[class*='upload' i] img,[data-testid*='preview' i] video,[data-testid*='media' i] video,[class*='preview' i] video,[class*='upload' i] video,canvas\")].filter(visible).filter((node)=>{const source=String(node.currentSrc||node.src||node.poster||'');const rect=node.getBoundingClientRect();return (node.tagName==='VIDEO'||node.tagName==='CANVAS'||/^(blob:|data:)/i.test(source)||rect.width>=64&&rect.height>=64)&&rect.width>=64&&rect.height>=64});" +
          "const signature=previews.map((node)=>String(node.currentSrc||node.src||node.poster||'')+'|'+Math.round(node.getBoundingClientRect().width)+'x'+Math.round(node.getBoundingClientRect().height)).join('|');" +
          "const text=(document.body?.innerText||'').slice(0,16000);return {selected:files.length===expected.length&&files.every((name,index)=>name===expected[index]),fileCount:files.length,previewCount:previews.length,signature,uploadError:/upload failed|upload error|上传失败|上传错误/i.test(text),processing:/uploading|processing|上传中|正在上传|处理中|转码中/i.test(text)};" +
        "})()")
        if (metaUploadState?.selected && metaUploadState.fileCount === metaExpectedMedia.length && metaUploadState.previewCount === metaExpectedMedia.length && metaUploadState.signature && !metaUploadState.uploadError && !metaUploadState.processing) {
          if (metaUploadState.signature === metaPreviousSignature) metaStableReads += 1
          else metaStableReads = 1
          metaPreviousSignature = metaUploadState.signature
          if (metaStableReads >= 2) break
        } else {
          metaStableReads = 0
          metaPreviousSignature = ""
        }
        await new Promise((resolve) => setTimeout(resolve, 400))
      }
      if (!metaUploadState?.selected || metaUploadState.fileCount !== metaExpectedMedia.length || metaUploadState.previewCount !== metaExpectedMedia.length || metaStableReads < 2 || metaUploadState.uploadError || metaUploadState.processing) {
        metaFail("meta_media_upload_unverified")
      }

      const metaCaptionSelector = await js("(() => {const root=document.querySelector('[data-alpha-meta-composer=\"true\"]');if(!root)return '';const visible=(node)=>{if(!node)return false;const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.visibility!=='hidden'&&s.display!=='none'&&r.width>0&&r.height>0&&node.getAttribute('aria-hidden')!=='true'};const nodes=[...root.querySelectorAll('[contenteditable=\"true\"],textarea,[role=\"textbox\"]')].filter(visible);if(nodes.length!==1)return nodes.length>1?'__ambiguous__':'';nodes[0].setAttribute('data-alpha-meta-caption','true');return '[data-alpha-meta-caption=\\\"true\\\"]';})()")
      if (metaCaptionSelector === "__ambiguous__") metaFail("meta_caption_input_ambiguous")
      if (!metaCaptionSelector || !metaExpectedCaption) metaFail("meta_caption_input_missing")
      if (metaCaptionSelector.includes("contenteditable") || metaCaptionSelector.includes("alpha-meta-caption")) {
        const isEditor = await js("(() => {const node=document.querySelector('[data-alpha-meta-caption=\"true\"]');return Boolean(node&&node.getAttribute('contenteditable')==='true')})()")
        if (isEditor) {
          if (!await fillRichEditor(metaCaptionSelector, metaExpectedCaption)) metaFail("meta_caption_fill_failed")
        } else {
          await fillInput(metaCaptionSelector, metaExpectedCaption)
        }
      } else {
        await fillInput(metaCaptionSelector, metaExpectedCaption)
      }
      const metaCaptionReadback = await js("(() => {const node=document.querySelector('[data-alpha-meta-caption=\"true\"]');const expected=" + JSON.stringify(metaExpectedCaption) + ";if(!node)return false;const actual=(node.value||node.innerText||node.textContent||'').replace(/\\r\\n?/g,'\\n');return actual===expected||actual===expected+'\\n';})()")
      if (!metaCaptionReadback) metaFail("meta_caption_readback")

      if (target.platformCode === "facebook") {
        if (!target.publishAt) metaFail("facebook_immediate_submit_unsupported")
        const metaExpectedSchedule = (() => {
          const instant = new Date(target.publishAt)
          if (Number.isNaN(instant.getTime()) || instant.getUTCSeconds() !== 0 || instant.getUTCMilliseconds() !== 0) return null
          const fields = Object.fromEntries(new Intl.DateTimeFormat('en-GB', {
            timeZone: 'Asia/Shanghai', calendar: 'gregory', year: 'numeric', month: '2-digit', day: '2-digit',
            hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
          }).formatToParts(instant).filter((part) => part.type !== 'literal').map((part) => [part.type, part.value]))
          const date = fields.year + '-' + fields.month + '-' + fields.day
          const time = fields.hour + ':' + fields.minute
          return { date, time, dateTimeMinute: date + ' ' + time, dateTimeLocal: date + 'T' + time }
        })()
        if (!metaExpectedSchedule || new Date(target.publishAt).getTime() <= Date.now()) metaFail("meta_schedule_time_invalid")
        const metaScheduleOptionsSelector = await metaUniqueControl(
          ["button", "[role='button']", "[role='group']"],
          ["Scheduling options", "排期选项", "定时选项"],
          "schedule-options",
        )
        if (metaScheduleOptionsSelector === "__ambiguous__") metaFail("meta_schedule_options_ambiguous")
        if (metaScheduleOptionsSelector) {
          await click(metaScheduleOptionsSelector, { label: "open Meta Scheduling options" })
          await new Promise((resolve) => setTimeout(resolve, 250))
        }
        const metaSetDateTimeSelector = await metaUniqueControl(
          ["input[type='checkbox']", "[role='switch']", "[role='checkbox']", "button", "[role='button']"],
          ["Set date and time", "设置日期和时间", "设定日期和时间"],
          "set-date-time",
        )
        if (metaSetDateTimeSelector === "__ambiguous__" || !metaSetDateTimeSelector) metaFail("meta_schedule_toggle_missing_or_ambiguous")
        const metaScheduleToggleChecked = async () => js("(() => {const node=document.querySelector('[data-alpha-meta-set-date-time=\"true\"]');if(!node)return null;const control=node.matches(\"input[type='checkbox'],[role='switch'],[role='checkbox']\")?node:node.querySelector(\"input[type='checkbox'],[role='switch'],[role='checkbox']\");if(!control)return null;if(control.checked===true||control.checked===false)return control.checked;const value=control.getAttribute('aria-checked')||control.getAttribute('data-checked');return value==='true'?true:value==='false'?false:null})()")
        if (await metaScheduleToggleChecked() !== true) {
          await click(metaSetDateTimeSelector, { label: "enable Meta Set date and time" })
          await new Promise((resolve) => setTimeout(resolve, 250))
        }
        if (await metaScheduleToggleChecked() !== true) metaFail("meta_schedule_mode_readback_mismatch")
        const metaScheduleInputs = await js("(() => {const root=document.querySelector('[data-alpha-meta-composer=\"true\"]');if(!root)return null;const visible=(node)=>{const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.visibility!=='hidden'&&s.display!=='none'&&r.width>0&&r.height>0};const nodes=[...root.querySelectorAll(\"input[type='datetime-local'],input[type='date'],input[type='time'],input[aria-label*='date' i],input[aria-label*='time' i],input[aria-label*='日期'],input[aria-label*='时间']\")].filter(visible);const dateTime=nodes.filter((node)=>node.type==='datetime-local');const dates=nodes.filter((node)=>node.type==='date'||/date|日期/i.test(node.getAttribute('aria-label')||''));const times=nodes.filter((node)=>node.type==='time'||/time|时间/i.test(node.getAttribute('aria-label')||''));if(dateTime.length===1){dateTime[0].setAttribute('data-alpha-meta-schedule-datetime','true');return {dateTime:'[data-alpha-meta-schedule-datetime=\"true\"]'}}if(dates.length===1&&times.length===1){dates[0].setAttribute('data-alpha-meta-schedule-date','true');times[0].setAttribute('data-alpha-meta-schedule-time','true');return {date:'[data-alpha-meta-schedule-date=\"true\"]',time:'[data-alpha-meta-schedule-time=\"true\"]'}}return null})()")
        if (!metaScheduleInputs) metaFail("meta_schedule_controls_missing_or_ambiguous")
        if (metaScheduleInputs.dateTime) {
          await fillInput(metaScheduleInputs.dateTime, metaExpectedSchedule.dateTimeLocal)
        } else {
          await fillInput(metaScheduleInputs.date, metaExpectedSchedule.date)
          await fillInput(metaScheduleInputs.time, metaExpectedSchedule.time)
        }
        let metaSchedulePrevious = ''
        let metaScheduleStable = false
        for (let attempt = 0; attempt < 2; attempt += 1) {
          const metaScheduleReadback = await js("(() => {const dateTime=document.querySelector('[data-alpha-meta-schedule-datetime=\"true\"]'),date=document.querySelector('[data-alpha-meta-schedule-date=\"true\"]'),time=document.querySelector('[data-alpha-meta-schedule-time=\"true\"]');return {dateTime:String(dateTime?.value||''),date:String(date?.value||''),time:String(time?.value||'')}})()")
          const signature = metaScheduleReadback.dateTime || (metaScheduleReadback.date + '|' + metaScheduleReadback.time)
          const exact = metaScheduleReadback.dateTime === metaExpectedSchedule.dateTimeLocal ||
            (metaScheduleReadback.date === metaExpectedSchedule.date && metaScheduleReadback.time === metaExpectedSchedule.time)
          if (exact && signature === metaSchedulePrevious && await metaScheduleToggleChecked() === true) metaScheduleStable = true
          metaSchedulePrevious = exact ? signature : ''
          await new Promise((resolve) => setTimeout(resolve, 350))
        }
        if (!metaScheduleStable) metaFail("meta_schedule_readback_mismatch")
      }

      const metaFinalSelector = await metaUniqueControl(
        ["button", "[role=\"button\"]", "input[type=\"button\"]", "input[type=\"submit\"]"],
        target.platformCode === "facebook" ? ["Schedule", "定时发布"] : ["Publish", "Share", "发布", "分享"],
        "final",
      )
      if (metaFinalSelector === "__ambiguous__") metaFail("meta_final_button_ambiguous")
      if (!metaFinalSelector) metaFail("meta_final_button_missing_or_disabled")
      const metaFinalReady = await js("(() => {const node=document.querySelector('[data-alpha-meta-final=\"true\"]');return Boolean(node&&!node.disabled&&node.getAttribute('aria-disabled')!=='true')})()")
      if (!metaFinalReady) metaFail("meta_final_button_missing_or_disabled")
      status = "awaiting_review"
      errorCode = ""
    } else if (target.platformCode === "tiktok") {
      const tikTokDeadlineAt = Date.now() + 90000
      const tikTokRemaining = () => tikTokDeadlineAt - Date.now()
      const clearTikTokMarkers = async () => js("(() => {" +
        "for (const node of document.querySelectorAll('*')) for (const name of node.getAttributeNames()) " +
        "if (name.startsWith('data-alpha-tiktok-') && name !== 'data-alpha-tiktok-stable') node.removeAttribute(name); return true;})()")
      const uniqueTikTokSelector = async (selectors, marker, visibleOnly = true) => {
        await clearTikTokMarkers()
        return js("(() => {" +
          "const selectors=" + JSON.stringify(selectors) + ",marker=" + JSON.stringify(marker) + ",visibleOnly=" + JSON.stringify(visibleOnly) + ";" +
          "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const nodes=[];for(const selector of selectors)for(const node of document.querySelectorAll(selector))if((!visibleOnly||visible(node))&&!node.disabled&&node.getAttribute('aria-disabled')!=='true')nodes.push(node);" +
          "const unique=[...new Set(nodes)];if(unique.length!==1)return unique.length>1?'__ambiguous__':'';" +
          "const stable=unique[0].getAttribute('data-alpha-tiktok-stable')||('n'+Math.random().toString(36).slice(2));" +
          "unique[0].setAttribute('data-alpha-tiktok-stable',stable);unique[0].setAttribute('data-alpha-tiktok-'+marker,'true');return '[data-alpha-tiktok-'+marker+'=\\\"true\\\"]';})()")
      }
      const uniqueTikTokNamedControl = async (labels, marker, dialogOnly = false) => js("(() => {" +
        "const labels=" + JSON.stringify(labels) + ",marker=" + JSON.stringify(marker) + ";" +
        "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const root=dialogOnly?[...document.querySelectorAll('[role=\\\"dialog\\\"]')].filter(visible):[document.body];" +
        "if(root.length!==1)return root.length>1?'__ambiguous__':'';" +
        "const controls=[...root[0].querySelectorAll('button,[role=\\\"button\\\"],input[type=\\\"button\\\"],label')].filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&labels.includes((node.innerText||node.textContent||node.getAttribute('aria-label')||'').trim()));" +
        "if(controls.length!==1)return controls.length>1?'__ambiguous__':'';const stable=controls[0].getAttribute('data-alpha-tiktok-stable')||('n'+Math.random().toString(36).slice(2));controls[0].setAttribute('data-alpha-tiktok-stable',stable);controls[0].setAttribute('data-alpha-tiktok-'+marker,'true');return '[data-alpha-tiktok-'+marker+'=\\\"true\\\"]';})()")
      const readTikTokBoolean = async (selector) => js("(() => {" +
        "const node=document.querySelector(" + JSON.stringify(selector) + ");if(!node)return null;" +
        "if(node.matches('input[type=\\\"checkbox\\\"]'))return Boolean(node.checked);" +
        "const aria=node.getAttribute('aria-checked');if(aria==='true'||aria==='false')return aria==='true';" +
        "const state=node.querySelector('input[type=\\\"checkbox\\\"],input.Checkbox__input,input.Switch__input,[aria-checked]');" +
        "if(state&&state.matches('input'))return Boolean(state.checked);" +
        "if(state){const value=state.getAttribute('aria-checked');if(value==='true'||value==='false')return value==='true';}" +
        "return null;})()")
      const setTikTokBoolean = async (controlSelector, expected, label) => {
        const before = await readTikTokBoolean(controlSelector)
        if (before === null) throw new Error("tiktok_" + label + "_state_unreadable")
        if (before !== expected) {
          await click(controlSelector, { label: "set TikTok " + label })
          await new Promise((resolve) => setTimeout(resolve, 180))
        }
        if (await readTikTokBoolean(controlSelector) !== expected) throw new Error("tiktok_" + label + "_readback_mismatch")
      }
      const exactTikTokText = async (texts, marker, enabledOnly = false) => js("(() => {" +
        "const texts=" + JSON.stringify(texts) + ",marker=" + JSON.stringify(marker) + ",enabledOnly=" + JSON.stringify(enabledOnly) + ";" +
        "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const enabled=(node)=>!enabledOnly||(!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&!node.closest('button[disabled],[aria-disabled=\\\"true\\\"]'));" +
        "const nodes=[...document.querySelectorAll('body *')].filter((node)=>visible(node)&&enabled(node)&&texts.includes((node.innerText||node.textContent||'').trim())&&[...node.children].every((child)=>(child.innerText||child.textContent||'').trim()!==(node.innerText||node.textContent||'').trim()));" +
        "if(nodes.length!==1)return nodes.length>1?'__ambiguous__':'';nodes[0].setAttribute('data-alpha-tiktok-'+marker,'true');return '[data-alpha-tiktok-'+marker+'=\\\"true\\\"]';})()")
      const dismissTikTokDialog = async (labels, errorCode) => {
        const dialogControlSelector = await uniqueTikTokNamedControl(labels, "dialog-dismiss", true)
        if (dialogControlSelector === "__ambiguous__") throw new Error(errorCode)
        if (!dialogControlSelector) return false
        await click(dialogControlSelector, { label: "dismiss TikTok dialog" })
        await new Promise((resolve) => setTimeout(resolve, 250))
        return true
      }

      if (target.contentType !== "video") throw new Error("tiktok_content_type_unsupported")
      const videoMedia = target.media.filter((item) => item.role === "video")
      const coverMedia = target.media.filter((item) => item.role === "cover")
      if (videoMedia.length !== 1 || coverMedia.length > 1 || videoMedia.length + coverMedia.length !== target.media.length || videoMedia[0].kind !== "video" || (coverMedia.length === 1 && coverMedia[0].kind !== "image")) {
        throw new Error("tiktok_media_invalid")
      }

      const stalePromptSelector = await exactTikTokText(["Past edited video was not saved. Continue editing?", "过往编辑的视频未保存。继续编辑?"], "stale-prompt")
      if (stalePromptSelector === "__ambiguous__") throw new Error("tiktok_stale_draft_ambiguous")
      if (stalePromptSelector) {
        const discardSelector = await js("(() => {" +
          "const prompt=document.querySelector('[data-alpha-tiktok-stale-prompt=\\\"true\\\"]');if(!prompt)return '';" +
          "let root=prompt;for(let depth=0;depth<6&&root;depth++,root=root.parentElement){" +
          "const controls=[...root.querySelectorAll('button,[role=\\\"button\\\"]')].filter((node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0&&!node.disabled&&['Discard','放弃'].includes((node.innerText||node.textContent||'').trim())});" +
          "if(controls.length===1){controls[0].setAttribute('data-alpha-tiktok-stale-discard','true');return '[data-alpha-tiktok-stale-discard=\\\"true\\\"]';}if(controls.length>1)return '__ambiguous__';}return '';})()")
        if (discardSelector === "__ambiguous__") throw new Error("tiktok_stale_discard_ambiguous")
        if (!discardSelector) throw new Error("tiktok_stale_discard_missing")
        await click(discardSelector, { label: "discard stale TikTok draft" })
        await new Promise((resolve) => setTimeout(resolve, 350))
        if (await exactTikTokText(["Past edited video was not saved. Continue editing?", "过往编辑的视频未保存。继续编辑?"], "stale-check")) throw new Error("tiktok_stale_draft_not_closed")
      }

      const uploadTrigger = await exactTikTokText(["Select video", "选择视频", "Select file", "选择文件"], "video-upload-trigger")
      if (uploadTrigger === "__ambiguous__" || !uploadTrigger) throw new Error("tiktok_video_upload_trigger_missing")
      await click(uploadTrigger, { label: "select TikTok video" })
      await new Promise((resolve) => setTimeout(resolve, 350))
      const videoInputSelector = await uniqueTikTokSelector(["input[type='file'][accept*='video' i]", "input[type='file'][accept*='.mp4' i]"], "video-input", false)
      if (videoInputSelector === "__ambiguous__" || !videoInputSelector) throw new Error("tiktok_video_input_missing_or_ambiguous")
      await uploadFile(videoInputSelector, videoMedia[0].filePath)
      const expectedVideoName = JSON.stringify(videoMedia[0].filePath.split(/[\\/]/).pop())
      let previousVideoSignature = "", stableVideoReads = 0, uploadState = null
      while (tikTokRemaining() > 1200) {
        uploadState = await js("(() => {" +
          "const expectedName=" + expectedVideoName + ";" +
          "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const input=document.querySelector('[data-alpha-tiktok-video-input=\\\"true\\\"]');" +
          "const previews=[...document.querySelectorAll('video,[data-e2e*=\\\"video\\\" i] video')].filter(visible).map((node)=>{const rect=node.getBoundingClientRect();return {source:String(node.currentSrc||node.src||node.poster||''),width:Math.round(rect.width),height:Math.round(rect.height),readyState:Number(node.readyState||0),videoWidth:Number(node.videoWidth||0),videoHeight:Number(node.videoHeight||0)}}).filter((item)=>item.width>=64&&item.height>=64&&(item.source||item.videoWidth>0&&item.videoHeight>0));" +
          "const signature=previews.map((item)=>[item.source,item.width,item.height,item.readyState,item.videoWidth,item.videoHeight].join(':')).join('|');" +
          "const text=(document.body?.innerText||'').slice(0,10000);" +
          "return {fileSelected:Boolean(input?.files?.length===1&&input.files[0].name===expectedName&&input.files[0].size>0),signature,preview:Boolean(signature),uploadError:/upload failed|upload error|上传失败|上传错误/i.test(text),processing:/uploading|processing|上传中|处理中|转码中/i.test(text)};})()")
        if (uploadState?.fileSelected && uploadState.preview && !uploadState.uploadError && !uploadState.processing) {
          if (uploadState.signature && uploadState.signature === previousVideoSignature) stableVideoReads++
          else stableVideoReads = 1
          previousVideoSignature = uploadState.signature
          if (stableVideoReads >= 2) break
        } else {
          stableVideoReads = 0
          previousVideoSignature = ""
        }
        await new Promise((resolve) => setTimeout(resolve, 400))
      }
      if (!uploadState?.fileSelected || !uploadState.preview || stableVideoReads < 2 || uploadState.uploadError || uploadState.processing) throw new Error("tiktok_video_upload_unverified")

      await dismissTikTokDialog(["Cancel", "取消"], "tiktok_post_upload_cancel_ambiguous")
      await dismissTikTokDialog(["Got it", "知道了"], "tiktok_post_upload_dismiss_ambiguous")

      if (coverMedia.length === 1) {
        const editCoverSelector = await exactTikTokText(["Edit cover", "编辑封面", "Select cover", "选择封面"], "cover-edit")
        if (editCoverSelector === "__ambiguous__" || !editCoverSelector) throw new Error("tiktok_native_cover_edit_missing")
        await click(editCoverSelector, { label: "edit TikTok native cover" })
        await new Promise((resolve) => setTimeout(resolve, 300))
        const saveCoverSelector = await uniqueTikTokNamedControl(["Save", "保存", "Confirm", "确认"], "cover-save", true)
        if (saveCoverSelector === "__ambiguous__" || !saveCoverSelector) throw new Error("tiktok_native_cover_confirm_missing")
        await click(saveCoverSelector, { label: "confirm TikTok native cover" })
        let coverClosed = false
        while (tikTokRemaining() > 1200) {
          coverClosed = await js("(() => {" +
            "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
            "const dialogs=[...document.querySelectorAll('[role=\\\"dialog\\\"]')].filter(visible);" +
            "const marker=document.querySelector('[data-alpha-tiktok-cover-edit=\\\"true\\\"]');return dialogs.length===0&&Boolean(marker)&&visible(marker);})()")
          if (coverClosed) break
          await new Promise((resolve) => setTimeout(resolve, 350))
        }
        if (!coverClosed) throw new Error("tiktok_native_cover_not_committed")
      }

      const titleSelector = await uniqueTikTokSelector(["input[placeholder*='title' i]", "input[placeholder*='标题']"], "title")
      if (target.title) {
        if (titleSelector === "__ambiguous__" || !titleSelector) throw new Error("tiktok_title_missing_or_ambiguous")
        await fillInput(titleSelector, target.title)
        const titleReadback = await js("(() => {const node=document.querySelector('[data-alpha-tiktok-title=\\\"true\\\"]');return (node?.value||node?.innerText||'') === " + JSON.stringify(target.title) + "})()")
        if (!titleReadback) throw new Error("tiktok_title_readback_mismatch")
      }
      const bodyValue = target.body || target.caption
      const bodySelector = await uniqueTikTokSelector(["div.public-DraftEditor-content[contenteditable='true']", "div[contenteditable='true'][data-placeholder]", "div[contenteditable='true'][aria-label*='caption' i]", "textarea[placeholder*='caption' i]", "textarea[placeholder*='描述']"], "caption")
      if (bodySelector === "__ambiguous__" || !bodySelector) throw new Error("tiktok_caption_missing_or_ambiguous")
      if (bodySelector.includes("contenteditable")) {
        if (!await fillRichEditor(bodySelector, bodyValue)) throw new Error("tiktok_caption_fill_failed")
      } else {
        await fillInput(bodySelector, bodyValue)
      }
      const captionReadback = await js("(() => {const node=document.querySelector('[data-alpha-tiktok-caption=\\\"true\\\"]');const actual=(node?.value||node?.innerText||node?.textContent||'').replace(/\\r\\n?/g,'\\n');const expected=" + JSON.stringify(bodyValue) + ";return actual===expected||actual===expected+'\\n'})()")
      if (!captionReadback) throw new Error("tiktok_caption_readback_mismatch")

      if (target.tags.length) {
        for (const tag of target.tags) {
          const trigger = await exactTikTokText(["Hashtags", "话题标签"], "hashtag-trigger")
          if (trigger === "__ambiguous__" || !trigger) throw new Error("tiktok_hashtag_trigger_missing")
          const before = await js("(() => {const node=document.querySelector('[data-alpha-tiktok-caption=\\\"true\\\"]');return node?.innerText||node?.textContent||node?.value||''})()")
          await click(trigger, { label: "open TikTok hashtag picker" })
          await new Promise((resolve) => setTimeout(resolve, 300))
          const markerAdded = await js("(() => {const node=document.querySelector('[data-alpha-tiktok-caption=\\\"true\\\"]');const after=node?.innerText||node?.textContent||node?.value||'';return after!==" + JSON.stringify(before) + " && /#\\s*$/.test(after)})()")
          if (!markerAdded) throw new Error("tiktok_hashtag_marker_missing")
          await typeText(tag)
          await pressKey("SPACE")
          await new Promise((resolve) => setTimeout(resolve, 500))
          const tokenReadback = await js("(() => {const root=document.querySelector('[data-alpha-tiktok-caption=\\\"true\\\"]');const expected=" + JSON.stringify(tag) + ";if(!root)return false;const tokens=[...root.querySelectorAll('[data-testid*=\\\"mention\\\" i],[data-e2e*=\\\"mention\\\" i],[data-testid*=\\\"hashtag\\\" i],[data-e2e*=\\\"hashtag\\\" i],.mention,.hashtag')].map((node)=>(node.innerText||node.textContent||'').trim().replace(/^#/,'')).filter(Boolean);return tokens.includes(expected)})()")
          if (!tokenReadback) throw new Error("tiktok_hashtag_token_readback_mismatch")
        }
      }

      const visibilitySelector = await uniqueTikTokSelector([".view-auth-container .Select__trigger", ".view-auth-container [role='combobox']"], "visibility")
      if (visibilitySelector === "__ambiguous__" || !visibilitySelector) throw new Error("tiktok_visibility_missing_or_ambiguous")
      const visibilityExpected = target.visibility === "public" ? ["Everyone", "所有人"] : target.visibility === "friends" ? ["Friends", "好友"] : target.visibility === "private" ? ["Only you", "仅自己"] : []
      if (!visibilityExpected.length) throw new Error("tiktok_visibility_unsupported")
      let visibilityValue = await js("(() => {const node=document.querySelector('[data-alpha-tiktok-visibility=\\\"true\\\"]');return (node?.innerText||node?.textContent||'').trim()})()")
      if (!visibilityExpected.includes(visibilityValue)) {
        await click(visibilitySelector, { label: "open TikTok visibility" })
        const optionSelector = await exactTikTokText(visibilityExpected, "visibility-option")
        if (optionSelector === "__ambiguous__" || !optionSelector) throw new Error("tiktok_visibility_option_missing")
        await click(optionSelector, { label: "set TikTok visibility" })
        visibilityValue = await js("(() => {const node=document.querySelector('[data-alpha-tiktok-visibility=\\\"true\\\"]');return (node?.innerText||node?.textContent||'').trim()})()")
      }
      if (!visibilityExpected.includes(visibilityValue)) throw new Error("tiktok_visibility_readback_mismatch")

      const resolveBoolean = async (selectors, names, marker, label) => {
        const selector = await uniqueTikTokSelector(selectors, marker)
        if (selector === "__ambiguous__") throw new Error("tiktok_" + label + "_ambiguous")
        if (selector) return selector
        const named = await js("(() => {" +
          "const names=" + JSON.stringify(names) + ",marker=" + JSON.stringify(marker) + ";" +
          "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const labels=[...document.querySelectorAll('label,button,[role=\\\"switch\\\"],[role=\\\"checkbox\\\"]')].filter((node)=>visible(node)&&names.includes((node.innerText||node.textContent||node.getAttribute('aria-label')||'').trim()));" +
          "const controls=labels.map((node)=>node.matches('input,[role=\\\"switch\\\"],[role=\\\"checkbox\\\"]')?node:node.querySelector('input[type=\\\"checkbox\\\"],[role=\\\"switch\\\"],[role=\\\"checkbox\\\"]')).filter(Boolean);" +
          "const unique=[...new Set(controls)];if(unique.length!==1)return unique.length>1?'__ambiguous__':'';const stable=unique[0].getAttribute('data-alpha-tiktok-stable')||('n'+Math.random().toString(36).slice(2));unique[0].setAttribute('data-alpha-tiktok-stable',stable);unique[0].setAttribute('data-alpha-tiktok-'+marker,'true');return '[data-alpha-tiktok-'+marker+'=\\\"true\\\"]';})()")
        if (named === "__ambiguous__") throw new Error("tiktok_" + label + "_ambiguous")
        if (!named) throw new Error("tiktok_" + label + "_missing")
        return named
      }
      const commentSelector = await resolveBoolean(["[role='switch'][aria-label*='comment' i]", "input[type='checkbox'][name*='comment' i]", "input.Checkbox__input[aria-label*='评论']"], ["Allow comments", "Comments", "允许评论", "评论"], "comment", "allow_comment")
      await setTikTokBoolean(commentSelector, target.allow_comment, "allow_comment")
      const duetSelector = await resolveBoolean(["[role='switch'][aria-label*='duet' i]", "input[type='checkbox'][name*='duet' i]"], ["Allow Duet", "允许合拍", "Reuse content", "复用内容"], "duet", "allow_duet")
      const duetControlKey = await js("(() => document.querySelector('[data-alpha-tiktok-duet=\\\"true\\\"]')?.getAttribute('data-alpha-tiktok-stable')||'')()")
      const stitchSelector = await resolveBoolean(["[role='switch'][aria-label*='stitch' i]", "input[type='checkbox'][name*='stitch' i]"], ["Allow Stitch", "允许拼接", "Reuse content", "复用内容"], "stitch", "allow_stitch")
      const stitchControlKey = await js("(() => document.querySelector('[data-alpha-tiktok-stitch=\\\"true\\\"]')?.getAttribute('data-alpha-tiktok-stable')||'')()")
      if (duetControlKey && duetControlKey === stitchControlKey && target.allow_duet !== target.allow_stitch) throw new Error("tiktok_reuse_content_split_unavailable")
      await setTikTokBoolean(duetSelector, target.allow_duet, "allow_duet")
      if (!duetControlKey || duetControlKey !== stitchControlKey) await setTikTokBoolean(stitchSelector, target.allow_stitch, "allow_stitch")
      const aiSelector = await resolveBoolean(["[role='switch'][aria-label*='AI-generated' i]", "input[type='checkbox'][name*='ai' i]", "input.Switch__input"], ["AI-generated content", "AI 生成内容", "AI 生成的内容"], "ai", "ai_generated")
      await setTikTokBoolean(aiSelector, target.ai_generated, "ai_generated")
      const downloadSelector = await resolveBoolean(["[role='switch'][aria-label*='download' i]", "input[type='checkbox'][name*='download' i]"], ["Allow downloads", "允许下载"], "download", "allow_download").catch((error) => {
        if (/tiktok_allow_download_missing/.test(String(error?.message || error))) return ""
        throw error
      })
      if (downloadSelector) {
        await setTikTokBoolean(downloadSelector, target.allow_download, "allow_download")
      } else if (target.allow_download) {
        throw new Error("tiktok_allow_download_missing")
      } else {
        const landmarks = await js("(() => {const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const preview=[...document.querySelectorAll('video')].filter(visible).length===1;const captions=[...document.querySelectorAll(\"div.public-DraftEditor-content[contenteditable='true'],div[contenteditable='true'][data-placeholder],textarea[placeholder*='caption' i],textarea[placeholder*='描述']\")].filter(visible);const visibility=[...document.querySelectorAll('.view-auth-container .Select__trigger,.view-auth-container [role=\\\"combobox\\\"]')].filter(visible);const posts=[...document.querySelectorAll('button,[role=\\\"button\\\"]')].filter((node)=>visible(node)&&['Post','发布'].includes((node.innerText||node.textContent||'').trim()));return preview&&captions.length===1&&visibility.length===1&&posts.length===1})()")
        if (!landmarks) throw new Error("tiktok_allow_download_absence_unreviewed")
      }

      const tiktokExpectedSchedule = (publishAt) => {
        const instant = new Date(publishAt)
        if (Number.isNaN(instant.getTime())) return null
        const fields = Object.fromEntries(new Intl.DateTimeFormat('en-GB', {
          timeZone: 'Asia/Shanghai', calendar: 'gregory', year: 'numeric', month: '2-digit', day: '2-digit',
          hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
        }).formatToParts(instant).filter((part) => part.type !== 'literal').map((part) => [part.type, part.value]))
        const year = Number(fields.year), month = Number(fields.month), day = Number(fields.day)
        const hour = Number(fields.hour), minute = Number(fields.minute)
        if (![year, month, day, hour, minute].every(Number.isInteger) || month < 1 || month > 12 || day < 1 || day > 31 || hour < 0 || hour > 23 || minute < 0 || minute > 59) return null
        const rounded = Math.ceil((hour * 60 + minute) / 5) * 5
        const dateParts = (value) => Object.fromEntries(new Intl.DateTimeFormat('en-GB', {
          timeZone: 'Asia/Shanghai', calendar: 'gregory', year: 'numeric', month: '2-digit', day: '2-digit',
        }).formatToParts(value).filter((part) => part.type !== 'literal').map((part) => [part.type, part.value]))
        const next = rounded >= 1440 ? dateParts(new Date(Date.UTC(year, month - 1, day) + 86400000)) : { year: String(year), month: String(month).padStart(2, '0'), day: String(day).padStart(2, '0') }
        const nextHour = rounded >= 1440 ? 0 : Math.floor(rounded / 60)
        const nextMinute = rounded >= 1440 ? 0 : rounded %% 60
        const date = String(next.year).padStart(4, '0') + '-' + String(next.month).padStart(2, '0') + '-' + String(next.day).padStart(2, '0')
        const time = String(nextHour).padStart(2, '0') + ':' + String(nextMinute).padStart(2, '0')
        return { date, time, dateTimeMinute: date + ' ' + time, dateTime: date + ' ' + time + ':00' }
      }
      const uniqueTikTokScheduleControl = async (marker) => js("(() => {" +
        "const marker=" + JSON.stringify(marker) + ";const attribute='data-alpha-tiktok-'+marker;for(const node of document.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);" +
        "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const labels=new Set(['Schedule','定时发布','预约发布']);const raw=[...document.querySelectorAll(\"input[type='radio'],[role='radio'],label\")].filter((node)=>visible(node)&&labels.has((node.innerText||node.textContent||node.getAttribute('aria-label')||'').normalize('NFKC').replace(/\\s+/g,' ').trim()));" +
        "const controls=raw.filter((node)=>!raw.some((other)=>other!==node&&other.contains(node)));if(controls.length!==1)return controls.length>1?'__ambiguous__':'';controls[0].setAttribute(attribute,'true');return '['+attribute+'=\\\"true\\\"]';})()")
      const tiktokScheduleInputSelectors = async () => js("(() => {" +
        "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const inputs=[...document.querySelectorAll('.scheduled-picker input.TUXTextInputCore-input')].filter(visible);if(inputs.length!==2)return inputs.length>2?'__ambiguous__':'';" +
        "let date='',time='';for(const node of inputs){const value=String(node.value||'');if(/^\\d{4}-\\d{2}-\\d{2}$/.test(value))date=node;else if(/^\\d{2}:\\d{2}$/.test(value))time=node;}if(!date||!time)return '';date.setAttribute('data-alpha-tiktok-schedule-date','true');time.setAttribute('data-alpha-tiktok-schedule-time','true');return {dateSelector:'[data-alpha-tiktok-schedule-date=\\\"true\\\"]',timeSelector:'[data-alpha-tiktok-schedule-time=\\\"true\\\"]'};})()")
      const tiktokScheduleReadbackStable = async (selectors, expected) => {
        let previous = ''
        for (let attempt = 0; attempt < 2; attempt += 1) {
          const readback = await js("(() => {const date=document.querySelector(" + JSON.stringify(selectors?.dateSelector || '') + "),time=document.querySelector(" + JSON.stringify(selectors?.timeSelector || '') + ");return {date:date?.value||'',time:time?.value||''}})()")
          if (readback?.date === expected.date && readback?.time === expected.time) {
            const signature = readback.date + '|' + readback.time
            if (signature === previous) return true
            previous = signature
          }
          await new Promise((resolve) => setTimeout(resolve, 350))
        }
        return false
      }
      const uniqueTikTokScheduleDay = async (expectedDate) => js("(() => {" +
        "const expected=" + JSON.stringify(expectedDate) + ";const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const cells=[...document.querySelectorAll('.calendar-wrapper .day')].filter((node)=>visible(node)&&!node.classList.contains('disabled')&&!node.getAttribute('aria-disabled'));const exact=cells.filter((node)=>[node.getAttribute('data-date'),node.getAttribute('date'),node.dataset?.date].includes(expected));let candidates=exact;" +
        "if(candidates.length!==1){const inputs=[...document.querySelectorAll('.scheduled-picker input.TUXTextInputCore-input')].filter(visible);const current=inputs.map((node)=>String(node.value||'')).find((value)=>/^\\d{4}-\\d{2}-\\d{2}$/.test(value));const base=new Date(String(current||expected)+'T12:00:00'),target=new Date(expected+'T12:00:00');if(Number.isNaN(base.getTime())||Number.isNaN(target.getTime()))return candidates.length>1?'__ambiguous__':'';const monthDelta=(target.getFullYear()-base.getFullYear())*12+target.getMonth()-base.getMonth();if(monthDelta<0||monthDelta>1)return '';const leading=new Date(base.getFullYear(),base.getMonth(),1).getDay(),daysInBase=new Date(base.getFullYear(),base.getMonth()+1,0).getDate();const index=leading+(monthDelta===0?target.getDate()-1:daysInBase+target.getDate()-1);candidates=index>=0&&index<cells.length?[cells[index]]:[];}if(candidates.length!==1)return candidates.length>1?'__ambiguous__':'';candidates[0].setAttribute('data-alpha-tiktok-schedule-day','true');return '[data-alpha-tiktok-schedule-day=\\\"true\\\"]';})()")
      let finalSelector = ""
      if (target.publishAt) {
        const expectedSchedule = tiktokExpectedSchedule(target.publishAt)
        if (!expectedSchedule || new Date(target.publishAt).getTime() <= Date.now()) throw new Error("tiktok_schedule_time_invalid")
        const scheduleSelector = await uniqueTikTokScheduleControl("schedule-mode")
        if (scheduleSelector === "__ambiguous__" || !scheduleSelector) throw new Error("tiktok_schedule_mode_missing_or_ambiguous")
        if (await readTikTokBoolean(scheduleSelector) !== true) {
          await click(scheduleSelector, { label: "select TikTok Schedule" })
          await new Promise((resolve) => setTimeout(resolve, 300))
        }
        if (await readTikTokBoolean(scheduleSelector) !== true) throw new Error("tiktok_schedule_mode_readback_mismatch")
        const scheduleInputs = await tiktokScheduleInputSelectors()
        if (!scheduleInputs || scheduleInputs === "__ambiguous__" || !scheduleInputs.dateSelector || !scheduleInputs.timeSelector) throw new Error("tiktok_schedule_controls_missing_or_ambiguous")
        const currentDate = await js("(() => document.querySelector(" + JSON.stringify(scheduleInputs.dateSelector) + ")?.value || '')()")
        if (currentDate !== expectedSchedule.date) {
          await click(scheduleInputs.dateSelector, { label: "open TikTok schedule date" })
          await new Promise((resolve) => setTimeout(resolve, 150))
          const daySelector = await uniqueTikTokScheduleDay(expectedSchedule.date)
          if (daySelector === "__ambiguous__" || !daySelector) throw new Error("tiktok_schedule_date_missing_or_ambiguous")
          await click(daySelector, { label: "select TikTok schedule date" })
          await new Promise((resolve) => setTimeout(resolve, 150))
        }
        const selectTikTokOption = async (selector, values, marker) => {
          const optionSelector = await js("(() => {" +
            "const selector=" + JSON.stringify(selector) + ",expected=" + JSON.stringify(values) + ",marker=" + JSON.stringify(marker) + ";for(const node of document.querySelectorAll('[data-alpha-tiktok-schedule-option]'))node.removeAttribute('data-alpha-tiktok-schedule-option');" +
            "const wanted=new Set(Array.isArray(expected)?expected:[expected]);const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const nodes=[...document.querySelectorAll(selector)].filter((node)=>visible(node)&&wanted.has((node.innerText||node.textContent||'').trim()));if(nodes.length!==1)return nodes.length>1?'__ambiguous__':'';nodes[0].setAttribute('data-alpha-tiktok-schedule-option',marker);return '[data-alpha-tiktok-schedule-option=\\\"'+marker+'\\\"]';})()")
          if (optionSelector === "__ambiguous__" || !optionSelector) return false
          await click(optionSelector, { label: "select TikTok schedule time" })
          await new Promise((resolve) => setTimeout(resolve, 120))
          return true
        }
        const [hour, minute] = expectedSchedule.time.split(':')
        const currentTime = await js("(() => document.querySelector(" + JSON.stringify(scheduleInputs.timeSelector) + ")?.value || '')()")
        if (currentTime !== expectedSchedule.time) {
          await click(scheduleInputs.timeSelector, { label: "open TikTok schedule time" })
          if (!await selectTikTokOption('.tiktok-timepicker-option-text.tiktok-timepicker-left', [hour, String(Number(hour))], 'hour')) throw new Error("tiktok_schedule_hour_missing_or_ambiguous")
          if (!await selectTikTokOption('.tiktok-timepicker-option-text.tiktok-timepicker-right', [minute, String(Number(minute))], 'minute')) throw new Error("tiktok_schedule_minute_missing_or_ambiguous")
        }
        if (!await tiktokScheduleReadbackStable(scheduleInputs, expectedSchedule)) throw new Error("tiktok_schedule_time_readback_mismatch")
        finalSelector = await js("(() => {const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const labels=new Set(['Schedule','定时发布','预约发布']);const buttons=[...document.querySelectorAll('button,[role=\\\"button\\\"],input[type=\\\"submit\\\"]')].filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&labels.has((node.innerText||node.textContent||node.value||'').trim()));if(buttons.length!==1)return buttons.length>1?'__ambiguous__':'';buttons[0].setAttribute('data-alpha-tiktok-final','true');return '[data-alpha-tiktok-final=\\\"true\\\"]';})()")
        if (finalSelector === "__ambiguous__") throw new Error("tiktok_schedule_final_button_ambiguous")
        if (!finalSelector) throw new Error("tiktok_schedule_final_button_missing_or_disabled")
      } else {
        while (tikTokRemaining() > 1200 && !finalSelector) {
          finalSelector = await exactTikTokText(["Post", "发布"], "final", true)
          if (finalSelector === "__ambiguous__") throw new Error("tiktok_final_post_ambiguous")
          if (!finalSelector) await new Promise((resolve) => setTimeout(resolve, 400))
        }
      }
      if (!finalSelector) throw new Error("tiktok_final_post_missing_or_disabled")
      const finalEnabled = await js("(() => {const node=document.querySelector('[data-alpha-tiktok-final=\\\"true\\\"]');return Boolean(node&&!node.disabled&&node.getAttribute('aria-disabled')!=='true')})()")
      if (!finalEnabled) throw new Error("tiktok_final_post_missing_or_disabled")
      status = "awaiting_review"
      errorCode = ""
	    } else if (target.platformCode === "wechat_video") {
      const wechatVideoDeadlineAt = Date.now() + 90000
      const wechatVideoRemaining = () => wechatVideoDeadlineAt - Date.now()
      const wechatVideoFramePrefix = "https://channels.weixin.qq.com/micro/content/post/create"
      const wechatVideoInputSelector = await js("(() => {" +
        "const inputs=[...document.querySelectorAll('input[type=\\\"file\\\"]')];" +
        "if(inputs.length!==1)return inputs.length>1?'__ambiguous__':'';" +
        "inputs[0].setAttribute('data-alpha-wechat-video-input','true');" +
        "return '[data-alpha-wechat-video-input=\\\"true\\\"]';})()")
      if (wechatVideoInputSelector === "__ambiguous__" || !wechatVideoInputSelector) throw new Error("wechat_video_input_missing_or_ambiguous")
      await uploadFile(wechatVideoInputSelector, target.media[0].filePath)
      const expectedVideoName = JSON.stringify(target.media[0].filePath.split(/[\\/]/).pop())
      let uploadState = null
      let previousVideoSignature = ""
      let stableVideoReads = 0
      while (wechatVideoRemaining() > 1200) {
        uploadState = await js("(() => {" +
          "const expectedName=" + expectedVideoName + ";" +
          "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(" + JSON.stringify("https://channels.weixin.qq.com/micro/content/post/create") + "));" +
          "let frameDocument=null;try{if(frames.length===1)frameDocument=frames[0].contentDocument}catch{};" +
          "const roots=frameDocument?[document,frameDocument]:[document];" +
          "const previewItems=roots.flatMap((root)=>[...root.querySelectorAll('video,img')]).filter(visible).map((node)=>{const rect=node.getBoundingClientRect();return {source:String(node.currentSrc||node.src||node.poster||''),width:Math.round(rect.width),height:Math.round(rect.height),tag:node.tagName}}).filter((item)=>item.width>=64&&item.height>=64&&(item.tag==='VIDEO'||/^(blob:|data:)/i.test(item.source)));" +
          "const signature=previewItems.map((item)=>[item.source,item.width,item.height,item.tag].join(':')).join('|');" +
          "const text=roots.map((root)=>root.body?.innerText||'').join('\\n').slice(0,12000);" +
          "const input=document.querySelector('[data-alpha-wechat-video-input=\\\"true\\\"]');" +
          "return {fileSelected:Boolean(input?.files?.length===1&&input.files[0].name===expectedName&&input.files[0].size>0),iframeCount:frames.length,frameReady:Boolean(frameDocument),preview:Boolean(signature),signature,uploadError:/上传失败|上传错误|upload failed|upload error/i.test(text),processing:/上传中|正在上传|处理中|转码中|uploading|processing/i.test(text)};})()")
        if (uploadState?.fileSelected && uploadState.iframeCount === 1 && uploadState.frameReady && uploadState.preview && !uploadState.uploadError && !uploadState.processing) {
          if (uploadState.signature && uploadState.signature === previousVideoSignature) stableVideoReads++
          else stableVideoReads = 1
          previousVideoSignature = uploadState.signature
          if (stableVideoReads >= 2) break
        } else {
          stableVideoReads = 0
          previousVideoSignature = ""
        }
        await new Promise((resolve) => setTimeout(resolve, 400))
      }
      if (!uploadState?.fileSelected || uploadState.iframeCount !== 1 || !uploadState.frameReady || !uploadState.preview || stableVideoReads < 2 || uploadState.uploadError || uploadState.processing) throw new Error("wechat_video_upload_unverified")

      let editorState = null
      while (wechatVideoRemaining() > 1200) {
        editorState = await js("(() => {" +
          "const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(" + JSON.stringify("https://channels.weixin.qq.com/micro/content/post/create") + "));" +
          "if(frames.length!==1)return {iframeCount:frames.length,ready:false};let frameDocument=null;try{frameDocument=frames[0].contentDocument}catch{};if(!frameDocument)return {iframeCount:1,ready:false};" +
          "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const unique=(selectors)=>[...new Set(selectors.flatMap((selector)=>[...frameDocument.querySelectorAll(selector)]).filter(visible))];" +
          "const title=unique([\"input[placeholder*='标题']\"]),body=unique([\".input-editor[data-placeholder='添加描述']\",\"div[contenteditable='true']\",\"textarea[placeholder*='描述']\"]);" +
          "return {iframeCount:1,titleCount:title.length,bodyCount:body.length,ready:title.length===1&&body.length===1};})()")
        if (editorState?.ready) break
        await new Promise((resolve) => setTimeout(resolve, 400))
      }
      if (!editorState?.ready) throw new Error("wechat_video_editor_frame_or_field_ambiguous")

      const expectedWechatBody = JSON.stringify(target.body || target.caption)
      const fieldsReadback = await js("(() => {" +
        "const expectedTitle=" + JSON.stringify(target.title) + ",expectedBody=" + expectedWechatBody + ";" +
        "const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(" + JSON.stringify("https://channels.weixin.qq.com/micro/content/post/create") + "));" +
        "if(frames.length!==1||!frames[0].contentDocument)return {ok:false};const frameDocument=frames[0].contentDocument;" +
        "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const unique=(selectors)=>[...new Set(selectors.flatMap((selector)=>[...frameDocument.querySelectorAll(selector)]).filter(visible))];" +
        "const titles=unique([\"input[placeholder*='标题']\"]),bodies=unique([\".input-editor[data-placeholder='添加描述']\",\"div[contenteditable='true']\",\"textarea[placeholder*='描述']\"]);" +
        "if(titles.length!==1||bodies.length!==1)return {ok:false};const title=titles[0],body=bodies[0];" +
        "const setValue=(node,value)=>{node.focus();if(node.matches('input,textarea')){const setter=Object.getOwnPropertyDescriptor(Object.getPrototypeOf(node),'value')?.set;if(setter)setter.call(node,value);else node.value=value;}else node.textContent=value;node.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:value}));};" +
        "setValue(title,expectedTitle);setValue(body,expectedBody);" +
        "const titleValue=title.value||title.innerText||title.textContent||'';const bodyValue=(body.value||body.innerText||body.textContent||'').replace(/\\r\\n?/g,'\\n');" +
        "return {ok:titleValue===expectedTitle&&(bodyValue===expectedBody||bodyValue===expectedBody+'\\n'),titleValue,bodyValue};})()")
      if (!fieldsReadback?.ok) throw new Error("wechat_video_text_readback_failed")

      for (const tag of target.tags) {
        const focused = await js("(() => {" +
          "const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(" + JSON.stringify("https://channels.weixin.qq.com/micro/content/post/create") + "));" +
          "if(frames.length!==1||!frames[0].contentDocument)return false;const frameDocument=frames[0].contentDocument;const bodies=[...new Set([\".input-editor[data-placeholder='添加描述']\",\"div[contenteditable='true']\",\"textarea[placeholder*='描述']\"].flatMap((selector)=>[...frameDocument.querySelectorAll(selector)]))];if(bodies.length!==1)return false;bodies[0].focus();return true;})()")
        if (!focused) throw new Error("wechat_video_topic_editor_missing")
        await typeText("#" + tag)
        await pressKey("SPACE")
        await new Promise((resolve) => setTimeout(resolve, 700))
        const tokenFound = await js("(() => {" +
          "const expected=" + JSON.stringify(tag) + ";const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(" + JSON.stringify("https://channels.weixin.qq.com/micro/content/post/create") + "));" +
          "if(frames.length!==1||!frames[0].contentDocument)return false;const frameDocument=frames[0].contentDocument;const normalize=(value)=>String(value||'').replace(/[\\s\\u200b\\uFEFF]+/g,'').replace(/^#+/,'');" +
          "const nodes=[...frameDocument.querySelectorAll('[data-topic],[data-topic-selected],[data-tag-selected],[data-mention],a,[role=\\\"link\\\"],span[contenteditable=\\\"false\\\"],[class*=\\\"topic\\\" i],[class*=\\\"tag\\\" i]')];" +
          "return nodes.some((node)=>{const text=normalize(node.innerText||node.textContent);if(text!==expected)return false;const attrs=[...node.attributes].map((attribute)=>attribute.name+'='+attribute.value).join(' ').toLowerCase();const className=String(node.className||'').toLowerCase();return attrs.includes('topic')||attrs.includes('mention')||attrs.includes('tag')||/(^|[-_])(topic|mention|hashtag|tag)([-_]|$)/.test(className)||node.getAttribute('contenteditable')==='false'||node.matches('a,[role=\\\"link\\\"]');});})()")
        if (!tokenFound) throw new Error("wechat_video_topic_token_missing")
      }

      const expectedSchedule = wechatVideoExpectedSchedule(target.publishAt)
      if (!expectedSchedule || new Date(target.publishAt).getTime() <= Date.now()) throw new Error("wechat_video_schedule_time_invalid")
      const scheduleSelector = await uniqueWechatVideoScheduleControl(["定时发表", "定时发布"], "schedule-mode")
      const immediateSelector = await uniqueWechatVideoScheduleControl(["立即发表", "立即发布"], "immediate-mode")
      if (scheduleSelector === "__ambiguous__" || immediateSelector === "__ambiguous__" || !scheduleSelector || !immediateSelector) {
        throw new Error("wechat_video_schedule_mode_missing_or_ambiguous")
      }
      if (await readWechatVideoBoolean(scheduleSelector) !== true) {
        if (!await clickWechatVideoFrameControl("schedule-mode")) throw new Error("wechat_video_schedule_mode_readback_mismatch")
        await new Promise((resolve) => setTimeout(resolve, 300))
      }
      if (await readWechatVideoBoolean(scheduleSelector) !== true || await readWechatVideoBoolean(immediateSelector) === true) {
        throw new Error("wechat_video_schedule_mode_readback_mismatch")
      }
      const scheduleInputSelector = await uniqueWechatVideoScheduleInput()
      if (scheduleInputSelector === "__ambiguous__" || !scheduleInputSelector ||
          !await setWechatVideoScheduleInput(scheduleInputSelector, expectedSchedule) ||
          !await wechatVideoScheduleReadbackStable(scheduleInputSelector, expectedSchedule, scheduleSelector)) {
        throw new Error("wechat_video_schedule_time_readback_mismatch")
      }

      const finalSelector = await js("(() => {" +
        "const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(" + JSON.stringify("https://channels.weixin.qq.com/micro/content/post/create") + "));" +
        "if(frames.length!==1||!frames[0].contentDocument)return '';const frameDocument=frames[0].contentDocument;const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const controls=[...frameDocument.querySelectorAll('button,[role=\\\"button\\\"]')].filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&(node.innerText||node.textContent||'').trim()==='发表');" +
        "if(controls.length!==1)return controls.length>1?'__ambiguous__':'';controls[0].setAttribute('data-alpha-wechat-video-final','true');return '[data-alpha-wechat-video-final=\\\"true\\\"]';})()")
      if (finalSelector === "__ambiguous__") throw new Error("wechat_video_final_button_ambiguous")
      if (!finalSelector) throw new Error("wechat_video_final_button_missing_or_disabled")
      const finalEnabled = await js("(() => {const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(" + JSON.stringify("https://channels.weixin.qq.com/micro/content/post/create") + "));if(frames.length!==1||!frames[0].contentDocument)return false;const node=frames[0].contentDocument.querySelector('[data-alpha-wechat-video-final=\\\"true\\\"]');return Boolean(node&&!node.disabled&&node.getAttribute('aria-disabled')!=='true')})()")
      if (!finalEnabled) throw new Error("wechat_video_final_button_missing_or_disabled")
	      status = "awaiting_review"
	      errorCode = ""
	    } else if (target.platformCode === "wechat_mp") {
	      const wechatMPDeadlineAt = Date.now() + 90000
	      const wechatMPRemaining = () => wechatMPDeadlineAt - Date.now()
	      const wechatMPUniqueSelector = async (selectors, marker, allowHidden = false) => js("(() => {" +
	        "const selectors=" + JSON.stringify(selectors) + ",marker=" + JSON.stringify(marker) + ",allowHidden=" + JSON.stringify(allowHidden) + ";" +
	        "const visible=(node)=>{if(allowHidden&&node.matches(\"input[type='file']\"))return true;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
	        "const nodes=[...new Set(selectors.flatMap((selector)=>[...document.querySelectorAll(selector)]).filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'))];" +
	        "if(nodes.length!==1)return nodes.length>1?'__ambiguous__':'';nodes[0].setAttribute('data-alpha-wechat-mp-'+marker,'true');return '[data-alpha-wechat-mp-'+marker+'=\\\"true\\\"]';})()")
	      const wechatMPTextControl = async (labels, marker) => js("(() => {" +
	        "const labels=" + JSON.stringify(labels) + ",marker=" + JSON.stringify(marker) + ";" +
	        "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
	        "const nodes=[...document.querySelectorAll('a,button,[role=\\\"button\\\"],li,[tabindex]')].filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&labels.includes((node.innerText||node.textContent||'').trim()));" +
	        "if(nodes.length!==1)return nodes.length>1?'__ambiguous__':'';nodes[0].setAttribute('data-alpha-wechat-mp-'+marker,'true');return '[data-alpha-wechat-mp-'+marker+'=\\\"true\\\"]';})()")
	      const wechatMPReadValue = async (selector) => js("(() => {" +
	        "const node=document.querySelector(" + JSON.stringify(selector) + ");if(!node)return null;return String(node.value??node.innerText??node.textContent??'').replace(/\\r\\n?/g,'\\n');})()")
	      const wechatMPSetField = async (selector, value, marker) => js("(() => {" +
	        "const node=document.querySelector(" + JSON.stringify(selector) + "),value=" + JSON.stringify(value) + ";if(!node)return false;" +
	        "const setter=(element,text)=>{element.focus();if(element.matches('input,textarea,select')){const prototype=Object.getPrototypeOf(element),descriptor=Object.getOwnPropertyDescriptor(prototype,'value');if(descriptor?.set)descriptor.set.call(element,text);else element.value=text;}else element.textContent=text;element.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:text}));element.dispatchEvent(new Event('change',{bubbles:true}));};" +
	        "const probe=Array.from(value)[0]||'';setter(node,probe);const observedProbe=String(node.value??node.innerText??node.textContent??'').replace(/\\r\\n?/g,'\\n');if(observedProbe!==probe)return false;setter(node,value);const observed=String(node.value??node.innerText??node.textContent??'').replace(/\\r\\n?/g,'\\n');return observed===value||observed===value+'\\n';})()")
	      const wechatMPReadBoolean = async (selector) => js("(() => {" +
	        "const node=document.querySelector(" + JSON.stringify(selector) + ");if(!node)return null;if(typeof node.checked==='boolean')return node.checked;const aria=node.getAttribute('aria-checked');if(aria==='true')return true;if(aria==='false')return false;return null;})()")

	      const homeState = await js("(() => {" +
	        "const text=(document.body?.innerText||'').slice(0,12000),url=String(location.href||'');" +
	        "return {login:/登录|请先登录|sign in|log in/i.test(text)||/login|signin|passport/i.test(url),verification:/扫码|二维码|微信验证|验证码|安全验证|captcha|challenge/i.test(text)||/captcha|verify|challenge/i.test(url)};})()")
	      if (homeState?.login) throw new Error("EGO_ACCOUNT_NOT_LOGGED_IN")
	      if (homeState?.verification) throw new Error("PLATFORM_VERIFICATION_REQUIRED")

	      const beforeTabs = await listTabs()
	      let createEntrySelector = await wechatMPTextControl(["文章", "图文消息", "写新图文"], "create-entry")
	      if (createEntrySelector === "__ambiguous__") throw new Error("wechat_mp_create_entry_ambiguous")
	      if (!createEntrySelector) throw new Error("wechat_mp_create_entry_missing")
	      await click(createEntrySelector, { label: "open new WeChat article" })
	      await new Promise((resolve) => setTimeout(resolve, 800))
	      try { await waitForNetworkIdle({ timeout: 8 }) } catch {}
      const beforeTabIDs = new Set(beforeTabs.map((tab) => String(tab.targetId ?? tab.id ?? "")))
      const newEditorTabs = (await listTabs()).filter((tab) => {
        const id = String(tab.targetId ?? tab.id ?? "")
        if (!id || beforeTabIDs.has(id)) return false
        try {
          const url = new URL(String(tab.url || ""))
          return url.hostname === "mp.weixin.qq.com" && url.pathname.includes("/cgi-bin/appmsg")
        } catch { return false }
      })
      if (newEditorTabs.length !== 1) throw new Error("wechat_mp_new_editor_tab_missing_or_ambiguous")
      await switchTab(newEditorTabs[0].targetId ?? newEditorTabs[0].id)
      const editorPage = await pageInfo()
      if (!String(editorPage?.url || "").includes("mp.weixin.qq.com/cgi-bin/appmsg")) throw new Error("wechat_mp_editor_url_invalid")
      const editorState = await js("(() => {const text=(document.body?.innerText||'').slice(0,12000),url=String(location.href||'');return {login:/登录|请先登录|sign in|log in/i.test(text)||/login|signin|passport/i.test(url),verification:/扫码|二维码|微信验证|验证码|安全验证|captcha|challenge/i.test(text)||/captcha|verify|challenge/i.test(url)};})()")
      if (editorState?.login) throw new Error("EGO_ACCOUNT_NOT_LOGGED_IN")
      if (editorState?.verification) throw new Error("PLATFORM_VERIFICATION_REQUIRED")

      const titleSelector = await wechatMPUniqueSelector([".title-editor__input .ProseMirror[contenteditable='true']", "input[placeholder*='标题']", "textarea[placeholder*='标题']"], "title")
      const bodySelector = await wechatMPUniqueSelector([".mock-iframe-body .ProseMirror[contenteditable='true']"], "body")
      if (titleSelector === "__ambiguous__" || bodySelector === "__ambiguous__") throw new Error("wechat_mp_editor_field_ambiguous")
      if (!titleSelector || !bodySelector) throw new Error("wechat_mp_editor_field_missing")
      if (!await wechatMPSetField(titleSelector, target.title, "title-write") || !await wechatMPSetField(bodySelector, target.body, "body-write")) throw new Error("wechat_mp_text_readback_failed")
      if (await wechatMPReadValue(titleSelector) !== target.title || await wechatMPReadValue(bodySelector) !== target.body) throw new Error("wechat_mp_text_readback_failed")

      const beforeBodyImageCount = await js("(() => document.querySelectorAll('.mock-iframe-body img').length)()")
      const insertImageSelector = await wechatMPUniqueSelector(["li.jsInsertIcon.img"], "insert-image")
      if (insertImageSelector === "__ambiguous__" || !insertImageSelector) throw new Error("wechat_mp_insert_image_missing_or_ambiguous")
      await click(insertImageSelector, { label: "insert article image" })
      const imageInputSelector = await wechatMPUniqueSelector(["input[type='file'][accept*='image' i]", "input[type='file']"], "image-input", true)
      if (imageInputSelector === "__ambiguous__" || !imageInputSelector) throw new Error("wechat_mp_image_input_missing_or_ambiguous")
      await uploadFile(imageInputSelector, target.media[0].filePath)
      const expectedImageName = target.media[0].filePath.split(/[\\/]/).pop()
      let imageUploadState = null
      let previousBodyImageSignature = ""
      let stableBodyImageReads = 0
      while (wechatMPRemaining() > 1200) {
        imageUploadState = await js("(() => {" +
          "const expectedName=" + JSON.stringify(expectedImageName) + ";const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const input=document.querySelector('[data-alpha-wechat-mp-image-input=\\\"true\\\"]'),images=[...document.querySelectorAll('.mock-iframe-body img')].filter(visible),signature=images.map((node)=>String(node.currentSrc||node.src||'')+':'+Math.round(node.getBoundingClientRect().width)+':'+Math.round(node.getBoundingClientRect().height)).join('|'),text=(document.body?.innerText||'').slice(0,12000);" +
          "return {fileSelected:Boolean(input?.files?.length===1&&input.files[0].name===expectedName&&input.files[0].size>0),imageCount:images.length,signature,uploadError:/上传失败|上传错误|upload failed|upload error/i.test(text),processing:/上传中|正在上传|处理中|uploading|processing/i.test(text)};})()")
        if (imageUploadState?.fileSelected && imageUploadState.imageCount > beforeBodyImageCount && imageUploadState.signature && !imageUploadState.uploadError && !imageUploadState.processing) {
          stableBodyImageReads = imageUploadState.signature === previousBodyImageSignature ? stableBodyImageReads + 1 : 1
          previousBodyImageSignature = imageUploadState.signature
          if (stableBodyImageReads >= 2) break
        } else {
          stableBodyImageReads = 0
          previousBodyImageSignature = ""
        }
        await new Promise((resolve) => setTimeout(resolve, 400))
      }
      if (!imageUploadState?.fileSelected || imageUploadState.imageCount <= beforeBodyImageCount || stableBodyImageReads < 2 || imageUploadState.uploadError || imageUploadState.processing) throw new Error("wechat_mp_body_image_unverified")

      const coverAreaSelector = await wechatMPUniqueSelector([".js_cover_btn_area"], "cover-area")
      if (coverAreaSelector === "__ambiguous__" || !coverAreaSelector) throw new Error("wechat_mp_cover_area_missing_or_ambiguous")
      await click(coverAreaSelector, { label: "choose article cover" })
      const fromBodySelector = await wechatMPTextControl(["从正文选择", "从正文中选择"], "cover-from-body")
      if (fromBodySelector === "__ambiguous__" || !fromBodySelector) throw new Error("wechat_mp_cover_from_body_missing_or_ambiguous")
      await click(fromBodySelector, { label: "choose cover from article" })
      const coverItemSelector = await wechatMPUniqueSelector([".appmsg_content_img_item"], "cover-item")
      if (coverItemSelector === "__ambiguous__" || !coverItemSelector) throw new Error("wechat_mp_cover_item_missing_or_ambiguous")
      await click(coverItemSelector, { label: "select article image cover" })
      const nextSelector = await wechatMPTextControl(["下一步"], "cover-next")
      if (nextSelector === "__ambiguous__") throw new Error("wechat_mp_cover_next_ambiguous")
      if (nextSelector) {
        await click(nextSelector, { label: "advance cover selection" })
        await new Promise((resolve) => setTimeout(resolve, 300))
      }
      const confirmSelector = await wechatMPTextControl(["确认", "确定", "完成"], "cover-confirm")
      if (confirmSelector === "__ambiguous__" || !confirmSelector) throw new Error("wechat_mp_cover_confirm_missing_or_ambiguous")
      await click(confirmSelector, { label: "confirm article cover" })
      let coverState = null
      while (wechatMPRemaining() > 1200) {
	        coverState = await js("(() => {const nodes=[...document.querySelectorAll('.js_cover_preview_new')].filter((node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0});return {count:nodes.length,submitted:nodes.length===1&&getComputedStyle(nodes[0]).backgroundImage&&getComputedStyle(nodes[0]).backgroundImage!=='none'};})()")
        if (coverState?.count === 1 && coverState.submitted) break
        await new Promise((resolve) => setTimeout(resolve, 400))
      }
      if (coverState?.count !== 1 || !coverState.submitted) throw new Error("wechat_mp_cover_preview_unverified")

      const authorSelector = await wechatMPUniqueSelector(["input[placeholder*='作者']", "input[name*='author' i]", "textarea[placeholder*='作者']"], "author")
      if (authorSelector === "__ambiguous__" || !authorSelector) throw new Error("wechat_mp_author_missing_or_ambiguous")
      if (!await wechatMPSetField(authorSelector, target.author, "author-write") || await wechatMPReadValue(authorSelector) !== target.author) throw new Error("wechat_mp_author_readback_failed")
      const commentSelector = await wechatMPUniqueSelector(["input.js_interaction_setting", "[role='checkbox'][aria-label*='留言']", "[role='switch'][aria-label*='留言']"], "comment")
      if (commentSelector === "__ambiguous__" || !commentSelector) throw new Error("wechat_mp_comment_setting_missing_or_ambiguous")
      let commentValue = await wechatMPReadBoolean(commentSelector)
      if (commentValue === null) throw new Error("wechat_mp_comment_readback_failed")
      if (commentValue) {
        await click(commentSelector, { label: "disable WeChat article comments" })
        commentValue = await wechatMPReadBoolean(commentSelector)
      }
      if (commentValue !== false) throw new Error("wechat_mp_comment_readback_failed")

      const originalSelector = await wechatMPUniqueSelector([".js_edit_ori input.js_ori_setting_checkbox", "[role='checkbox'][aria-label*='原创']", "[role='switch'][aria-label*='原创']"], "original")
      if (originalSelector === "__ambiguous__") throw new Error("wechat_mp_original_setting_ambiguous")
      if (originalSelector) {
        let originalValue = await wechatMPReadBoolean(originalSelector)
        if (originalValue === null) throw new Error("wechat_mp_original_readback_failed")
        if (originalValue) {
          await click(originalSelector, { label: "disable WeChat article original declaration" })
          originalValue = await wechatMPReadBoolean(originalSelector)
        }
        if (originalValue !== false) throw new Error("wechat_mp_original_readback_failed")
      } else {
        const originalStateSelector = await wechatMPUniqueSelector([".js_edit_ori .js_unset_original_title"], "original-state")
        if (originalStateSelector === "__ambiguous__" || !originalStateSelector || (await wechatMPReadValue(originalStateSelector)).trim() !== "未声明") throw new Error("wechat_mp_original_readback_failed")
      }

      const collectionStateSelector = await wechatMPUniqueSelector([".js_article_tags_content"], "collection-state")
      if (collectionStateSelector === "__ambiguous__" || !collectionStateSelector || (await wechatMPReadValue(collectionStateSelector)).trim() !== "未添加") throw new Error("wechat_mp_collection_readback_failed")
      const sourceStateSelector = await wechatMPUniqueSelector([".js_claim_source_selected"], "content-source-state")
      if (sourceStateSelector === "__ambiguous__" || !sourceStateSelector || (await wechatMPReadValue(sourceStateSelector)).trim() !== "无需声明") throw new Error("wechat_mp_content_source_readback_failed")
      if (target.publishType !== "publish") throw new Error("wechat_mp_publish_type_unsupported")

      const advanceSelector = await wechatMPTextControl(["发表"], "advance")
      if (advanceSelector === "__ambiguous__") throw new Error("wechat_mp_advance_button_ambiguous")
      if (!advanceSelector) throw new Error("wechat_mp_advance_button_missing_or_disabled")
      await click(advanceSelector, { label: "open WeChat publish options" })
      await new Promise((resolve) => setTimeout(resolve, 500))
      const optionsDialogSelector = await wechatMPUniqueSelector([".new_mass_send_dialog .weui-desktop-dialog"], "options-dialog")
      if (optionsDialogSelector === "__ambiguous__" || !optionsDialogSelector) throw new Error("wechat_mp_publish_options_missing_or_ambiguous")
      const wechatMPDialogBoolean = async (label, marker, absentWhenFalse = false) => js("(() => {" +
        "const dialog=document.querySelector('[data-alpha-wechat-mp-options-dialog=\\\"true\\\"]'),label=" + JSON.stringify(label) + ",marker=" + JSON.stringify(marker) + ",absentWhenFalse=" + JSON.stringify(absentWhenFalse) + ";if(!dialog)return '';" +
        "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
		"const labels=[...dialog.querySelectorAll('label,span,div')].filter((node)=>visible(node)&&(node.innerText||node.textContent||'').trim()===label);" +
		"const rows=[...new Set(labels.map((node)=>node.closest('.mass-send__td')||node.parentElement).filter(Boolean))];" +
		"const controls=[...new Set(rows.flatMap((row)=>[...row.querySelectorAll(\"input[type='checkbox'],[role='checkbox'],[role='switch']\")]).filter(visible))];" +
        "if(controls.length===0&&absentWhenFalse)return '__absent__';if(controls.length!==1)return controls.length>1?'__ambiguous__':'';controls[0].setAttribute('data-alpha-wechat-mp-'+marker,'true');return '[data-alpha-wechat-mp-'+marker+'=\\\"true\\\"]';})()")
		for (const option of [{ label: "群发通知", marker: "notify", optional: false }, { label: "分组通知", marker: "group-notify", optional: true }, { label: "定时发表", marker: "schedule", optional: false }]) {
			const optionSelector = await wechatMPDialogBoolean(option.label, option.marker, option.optional)
			if (optionSelector === "__ambiguous__" || (!optionSelector && !option.optional)) throw new Error("wechat_mp_publish_option_missing_or_ambiguous")
			if (!optionSelector || optionSelector === "__absent__") continue
			let value = await wechatMPReadBoolean(optionSelector)
			if (value === null) throw new Error("wechat_mp_publish_option_readback_failed")
			if (value) {
				await click(optionSelector, { label: "disable WeChat " + option.label })
				value = await wechatMPReadBoolean(optionSelector)
        }
        if (value !== false) throw new Error("wechat_mp_publish_option_readback_failed")
      }
      const finalSelector = await js("(() => {const dialog=document.querySelector('[data-alpha-wechat-mp-options-dialog=\\\"true\\\"]');if(!dialog)return '';const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const controls=[...dialog.querySelectorAll('button.weui-desktop-btn_primary')].filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&(node.innerText||node.textContent||'').trim()==='发表');if(controls.length!==1)return controls.length>1?'__ambiguous__':'';controls[0].setAttribute('data-alpha-wechat-mp-final','true');return '[data-alpha-wechat-mp-final=\\\"true\\\"]';})()")
      if (finalSelector === "__ambiguous__") throw new Error("wechat_mp_final_button_ambiguous")
      if (!finalSelector) throw new Error("wechat_mp_final_button_missing_or_disabled")
      const finalEnabled = await js("(() => {const node=document.querySelector('[data-alpha-wechat-mp-final=\\\"true\\\"]');return Boolean(node&&!node.disabled&&node.getAttribute('aria-disabled')!=='true')})()")
      if (!finalEnabled) throw new Error("wechat_mp_final_button_missing_or_disabled")
      status = "awaiting_review"
      errorCode = ""
	    } else if (target.platformCode === "csdn") {
      if (target.media.length) {
        errorCode = "EGO_CSDN_COVER_REQUIRES_REVIEW"
	  } else if (!target.tags.length) {
		errorCode = "EGO_CSDN_TAG_REQUIRED"
      } else {
		const activated = await js("(() => {const title=document.querySelector('.article-bar__title-display');if(!title)return Boolean(document.querySelector(\"input[placeholder*='标题'],textarea[placeholder*='标题'],#txtTitle\"));title.click();return true})()")
        if (!activated) throw new Error("title_activation_missing")
        await new Promise((resolve) => setTimeout(resolve, 200))
		const titleSelector = await js("(() => {const selectors=[\"input[placeholder*='标题']\",\"textarea[placeholder*='标题']\",'#txtTitle'];return selectors.find((selector)=>{const node=document.querySelector(selector);if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.display!=='none'&&style.visibility!=='hidden'&&rect.width>0&&rect.height>0})||''})()")
		if (!titleSelector) throw new Error("title_fill_failed")
		await fillInput(titleSelector, target.title)
        if (!await fillRichEditor("pre.editor__inner[contenteditable='true']", target.body)) throw new Error("body_fill_failed")
        const titleReadback = await js("(() => {const node=document.querySelector(" + JSON.stringify(titleSelector) + ");return node?.value||''})()")
        if (titleReadback !== target.title) throw new Error("title_readback_failed")
		const advanceReady = await js("(() => {const visible=(node)=>{const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.display!=='none'&&s.visibility!=='hidden'&&r.width>0&&r.height>0};const controls=[...document.querySelectorAll('button.btn.btn-publish:not(.btn-b-red)')].filter(visible);return controls.length===1&&!controls[0].disabled})()")
        if (!advanceReady) throw new Error("options_button_missing")
        await click('button.btn.btn-publish:not(.btn-b-red)', { label: 'open publish options' })
        await new Promise((resolve) => setTimeout(resolve, 700))
		const optionsReady = await js("(() => {const visible=(node)=>{const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.display!=='none'&&s.visibility!=='hidden'&&r.width>0&&r.height>0};const dialog=document.querySelector('.publish-article-modal__body');const finals=[...document.querySelectorAll('button.btn-b-red')].filter((node)=>visible(node)&&node.textContent.trim()==='发布文章');return Boolean(dialog)&&finals.length===1})()")
        if (!optionsReady) throw new Error("final_button_missing")
		{
		  const triggerOpened = await js("(() => {const visible=(node)=>{const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.display!=='none'&&s.visibility!=='hidden'&&r.width>0&&r.height>0};const candidates=[...document.querySelectorAll('.publish-article-modal__body *')].filter((node)=>visible(node)&&node.textContent.trim()==='添加文章标签');if(candidates.length!==1)return false;candidates[0].click();return true})()")
          if (!triggerOpened) throw new Error("tag_picker_missing")
          await new Promise((resolve) => setTimeout(resolve, 300))
          const tagSelector = "input[placeholder='请输入文字搜索，Enter键入可添加自定义标签']"
          for (const tag of target.tags) {
			await fillInput(tagSelector, tag)
            await pressKey('ENTER')
            await new Promise((resolve) => setTimeout(resolve, 180))
          }
          const tagsReadback = await js("(() => {const text=document.querySelector('.mark_selection_box')?.innerText||'';return " + JSON.stringify(target.tags) + ".every((tag)=>text.includes(tag))})()")
          if (!tagsReadback) throw new Error("tag_readback_failed")
		  const closed = await js("(() => {const button=document.querySelector(\".mark_selection_box button[aria-label='关闭']\");if(!button)return false;button.click();return true})()")
          if (!closed) throw new Error("tag_picker_close_failed")
        }
		await new Promise((resolve) => setTimeout(resolve, 250))
		const finalEnabled = await js("(() => {const visible=(node)=>{const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.display!=='none'&&s.visibility!=='hidden'&&r.width>0&&r.height>0};const finals=[...document.querySelectorAll('button.btn-b-red')].filter((node)=>visible(node)&&node.textContent.trim()==='发布文章');return finals.length===1&&!finals[0].disabled})()")
		if (!finalEnabled) throw new Error("final_button_disabled")
        status = "awaiting_review"
        errorCode = ""
      }
    } else if (target.platformCode === "douyin") {
      if (target.media.length === 0) {
        errorCode = "EGO_PUBLISH_MEDIA_REQUIRED"
      } else if ((target.contentType === "graph_text" && (target.media.length > 30 || target.media.some((item) => item.kind !== "image"))) ||
                 (target.contentType === "video" && (target.media.length !== 1 || target.media[0]?.kind !== "video")) ||
                 (target.contentType !== "graph_text" && target.contentType !== "video")) {
        errorCode = "EGO_DOUYIN_MEDIA_INVALID"
      } else {
        if (target.contentType === "graph_text") {
          prepareStage = "home_route"
          const home = await pageInfo()
          if (!douyinRoute(home?.url, "/creator-micro/home")) throw new Error("douyin_home_route_unverified")
          prepareStage = "entry_or_fallback"
          const entrySelector = await js("(() => {"+
            "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};"+
            "const normalize=(value)=>String(value||'').replace(/\\s+/g,' ').trim();"+
            "const leaves=[...document.querySelectorAll('body *')].filter((node)=>visible(node)&&normalize(node.innerText||node.textContent)==='发布图文'&&![...node.children].some((child)=>visible(child)&&normalize(child.innerText||child.textContent)==='发布图文'));"+
            "const controls=[...new Set(leaves.map((node)=>node.closest('button,a,[role=\\\"button\\\"],[role=\\\"link\\\"]')).filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'))];"+
            "if(controls.length!==1)return '';controls[0].setAttribute('data-alpha-douyin-graph-entry','true');return '[data-alpha-douyin-graph-entry=\\\"true\\\"]';})()")
          let entryClickSucceeded = false
          if (entrySelector) {
            try {
              await click(entrySelector, { label: "open Douyin graph text entry" })
              entryClickSucceeded = true
            } catch {}
          }
          let reachedUpload = false
          if (entryClickSucceeded) {
            for (let attempt = 0; attempt < 24 && remainingPublishBudget() > 1200; attempt += 1) {
              const entryPage = await pageInfo()
              reachedUpload = douyinRoute(entryPage?.url, "/creator-micro/content/upload")
              if (reachedUpload) break
              await new Promise((resolve) => setTimeout(resolve, 300))
            }
          }
          if (!reachedUpload) {
            await openOrReuseTab("https://creator.douyin.com/creator-micro/content/upload?default-tab=3", { wait: true, timeout: 8 })
            for (let attempt = 0; attempt < 24 && remainingPublishBudget() > 1200; attempt += 1) {
              const fallbackPage = await pageInfo()
              reachedUpload = douyinRoute(fallbackPage?.url, "/creator-micro/content/upload")
              if (reachedUpload) break
              await new Promise((resolve) => setTimeout(resolve, 300))
            }
          }
          if (!reachedUpload) throw new Error("douyin_graph_upload_route_missing")
          prepareStage = "image_input"
          let imageInputSelector = ""
          for (let attempt = 0; attempt < 24 && remainingPublishBudget() > 1200; attempt += 1) {
            imageInputSelector = await js("(() => {"+
              "const inputs=[...document.querySelectorAll(\"input[type='file']\")];"+
              "const candidates=inputs.filter((node)=>/image|png|jpe?g|webp/i.test(String(node.accept||'')));"+
              "if(candidates.length!==1)return '';candidates[0].setAttribute('data-alpha-douyin-image-input','true');return '[data-alpha-douyin-image-input=\\\"true\\\"]';})()")
            if (imageInputSelector) break
            await new Promise((resolve) => setTimeout(resolve, 300))
          }
          if (!imageInputSelector) throw new Error("douyin_image_file_input_missing_or_ambiguous")
		  const expectedImageNames = target.media.map((item) => {
			const segments = String(item?.filePath || '').split(/[\\/]/)
			return segments[segments.length - 1]
		  })
		  if (target.media.length > 1) {
			prepareStage = "image_input_multiple"
			const imageInputAcceptsMultiple = await js("(() => {const node=document.querySelector(" + JSON.stringify(imageInputSelector) + ");return Boolean(node && node.multiple === true)})()")
			if (!imageInputAcceptsMultiple) throw new Error("douyin_image_file_input_multiple_unsupported")
		  }
          prepareStage = "upload"
		  await uploadFile(imageInputSelector, target.media.map((item) => item.filePath))
		  prepareStage = "upload_readback"
		  const imageFileReadback = await js("(() => {const node=document.querySelector(" + JSON.stringify(imageInputSelector) + ");if(!node)return {exists:false,names:[]};return {exists:true,names:[...(node.files||[])].map((file)=>String(file.name||''))}})()")
		  if (imageFileReadback?.exists && (!Array.isArray(imageFileReadback.names) || imageFileReadback.names.length !== expectedImageNames.length || imageFileReadback.names.some((name,index) => name !== expectedImageNames[index]))) {
			throw new Error("douyin_image_upload_file_readback_mismatch")
		  }
          prepareStage = "required_image_route"
          let reachedImagePost = false
          for (let attempt = 0; attempt < 24 && remainingPublishBudget() > 1200; attempt += 1) {
            const uploadedPage = await pageInfo()
            reachedImagePost = douyinRoute(uploadedPage?.url, "/creator-micro/content/post/image")
            if (reachedImagePost) break
            await new Promise((resolve) => setTimeout(resolve, 400))
          }
          if (!reachedImagePost) throw new Error("douyin_required_image_route_missing")
		  prepareStage = "upload_complete"
		  let imageUploadComplete = false
		  for (let attempt = 0; attempt < 24 && remainingPublishBudget() > 1200; attempt += 1) {
			const uploading = await js("(() => /上传中|正在上传|处理中|uploading|processing/i.test((document.body?.innerText||'').slice(0,8000)))()")
			if (!uploading) { imageUploadComplete = true; break }
			await new Promise((resolve) => setTimeout(resolve, 400))
		  }
		  if (!imageUploadComplete) throw new Error("douyin_upload_still_in_progress")
        } else {
          prepareStage = "upload"
          const inputSelector = 'input[type="file"]'
          await uploadFile(inputSelector, target.media[0].filePath)
        }
        let fields = null
		prepareStage = "controls"
		for (let attempt = 0; attempt < 24 && remainingPublishBudget() > 1200; attempt += 1) {
		  fields = await js("(() => {for(const node of document.querySelectorAll('[data-alpha-douyin-title],[data-alpha-douyin-body]')){node.removeAttribute('data-alpha-douyin-title');node.removeAttribute('data-alpha-douyin-body')}const visible=(node)=>{if(!node)return false;const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.display!=='none'&&s.visibility!=='hidden'&&r.width>0&&r.height>0};const unique=(selectors,attribute)=>{const nodes=[...document.querySelectorAll(selectors)].filter(visible);if(nodes.length!==1)return {selector:'',tag:''};nodes[0].setAttribute(attribute,'true');return {selector:'['+attribute+'=\\\"true\\\"]',tag:nodes[0].tagName.toLowerCase()}};const title=unique(\"input[placeholder='添加作品标题'],input[placeholder*='标题']\",'data-alpha-douyin-title');const body=unique(\"div[contenteditable='true'],textarea[placeholder*='简介'],textarea[placeholder*='描述']\",'data-alpha-douyin-body');return {title:title.selector,body:body.selector,bodyTag:body.tag,text:(document.body?.innerText||'').slice(0,4000)}})()")
          if (fields?.title && fields?.body && !/上传中|正在上传|处理中/.test(fields.text)) break
		  await new Promise((resolve) => setTimeout(resolve, 500))
        }
		if (!fields?.title || !fields?.body) {
		  if (remainingPublishBudget() <= 1200) errorCode = "EGO_PUBLISH_PREPARE_TIMEOUT"
		  else throw new Error("composer_fields_missing")
		}
		douyinDiagnosticBodySelector = fields.body
		if (errorCode === "EGO_PUBLISH_PREPARE_TIMEOUT") {
		  preparationResults.push({ platformCode: target.platformCode, status, errorCode })
		  continue
		}
		prepareStage = "title"
		await fillInput(fields.title, target.title)
		const expectedBody = target.expectedBody ?? target.body ?? target.caption ?? ""
		prepareStage = "body"
		if (fields.bodyTag === 'textarea') {
		  await fillInput(fields.body, expectedBody)
		} else if (target.platformCode === "douyin") {
		  if (!await fillDouyinRichEditor(fields.body, expectedBody)) throw new Error("douyin_body_fill_failed")
		} else if (!await fillRichEditor(fields.body, expectedBody)) {
		  throw new Error("body_fill_failed")
		}
		let expectedTopics = []
		if (douyinGraphText) {
		  prepareStage = "topics"
		  await new Promise((resolve) => setTimeout(resolve, 300))
		  let beforeTopics
		  try {
		    beforeTopics = await douyinBodyAndTopicReadback(fields.body)
		  } catch {
		    throw new Error("douyin_topic_preflight_readback_runtime_failed")
		  }
		  if (!beforeTopics || beforeTopics.invalid || !Array.isArray(beforeTopics.topics)) {
		    throw new Error("douyin_topic_preflight_readback_invalid")
		  }
		  if (beforeTopics.topics.length !== 0) {
		    throw new Error("douyin_topic_preflight_topics_present")
		  }
		  if (!douyinCandidateMatches(beforeTopics.bodyCandidates, expectedBody, normalizeDouyinBody)) {
		    throw new Error("douyin_topic_preflight_body_mismatch")
		  }
		  expectedTopics = await commitDouyinNativeTopics(fields.body, target.tags, expectedBody)
		}
		const contentReadback = await js("(() => {"+
		  "const titleNode=document.querySelector("+JSON.stringify(fields.title)+");return {titleCandidates:[titleNode?.value,titleNode?.innerText,titleNode?.textContent]};})()")
		prepareStage = "title"
		if (!douyinCandidateMatches(contentReadback?.titleCandidates, target.title, normalizeDouyinTitle)) throw new Error("douyin_title_readback_mismatch")
		prepareStage = "body"
		if (douyinGraphText) douyinTopicContext.stage = "readback"
		let bodyAndTopics
		try {
		  bodyAndTopics = await douyinBodyAndTopicReadback(fields.body)
		} catch {
		  throw new Error("douyin_body_readback_runtime_failed")
		}
		if (Array.isArray(bodyAndTopics?.topics)) douyinTopicContext.topics = bodyAndTopics.topics.slice(0, 4)
		if (!douyinCandidateMatches(bodyAndTopics?.bodyCandidates, expectedBody, normalizeDouyinBody)) throw new Error("douyin_body_readback_mismatch")
		if (douyinGraphText) {
		  prepareStage = "topics"
		  if (bodyAndTopics?.invalid || !Array.isArray(bodyAndTopics?.topics)) throw new Error("douyin_topic_readback_invalid")
		  if (expectedTopics.length > 0 && bodyAndTopics.topics.length === 0) throw new Error("douyin_topic_readback_empty")
		  if (bodyAndTopics.topics.length !== expectedTopics.length) throw new Error("douyin_topic_readback_count_mismatch")
		  if (!douyinExactTopicSetMatches(bodyAndTopics.topics, expectedTopics)) throw new Error("douyin_topic_readback_value_mismatch")
		}
		if (target.publishAt) {
		  prepareStage = "schedule_mode"
		  const expectedSchedule = douyinExpectedSchedule(target.publishAt)
		  if (!expectedSchedule || new Date(target.publishAt).getTime() <= Date.now()) throw new Error("douyin_schedule_time_invalid")
		  const scheduleSelector = await uniqueDouyinScheduleControl(["定时发布"], "mode-selector")
		  if (!scheduleSelector) throw new Error("douyin_schedule_mode_selector_missing_or_ambiguous")
		  if (!await douyinScheduleControlChecked(scheduleSelector)) {
		    const scheduleClicked = await js("(() => {const node=document.querySelector(" + JSON.stringify(scheduleSelector) + ");if(!node)return false;node.click();return true})()")
		    if (!scheduleClicked) throw new Error("douyin_schedule_mode_click_failed")
		    await new Promise((resolve) => setTimeout(resolve, 300))
		  }
		  if (!await douyinScheduleControlChecked(scheduleSelector)) throw new Error("douyin_schedule_mode_readback_mismatch")
		  const scheduleTimeSelector = await uniqueDouyinScheduleTimeInput()
		  if (!scheduleTimeSelector) throw new Error("douyin_schedule_time_selector_missing_or_ambiguous")
		  prepareStage = "schedule_time"
		  await fillInput(scheduleTimeSelector, expectedSchedule)
		  const scheduleTimeReadback = await js("(() => document.querySelector('[data-alpha-douyin-schedule-time=\\\"true\\\"]')?.value || '')()")
		  if (scheduleTimeReadback !== expectedSchedule) throw new Error("douyin_schedule_time_readback_mismatch")
		  const immediateSelector = await uniqueDouyinScheduleControl(["立即发布"], "immediate-selector")
		  if (!immediateSelector || await douyinScheduleControlChecked(immediateSelector)) throw new Error("douyin_schedule_immediate_readback_mismatch")
		}
		prepareStage = "final_control"
		const finalSelector = await uniqueDouyinFinalButton("final-button")
		if (!finalSelector || !await douyinFinalButtonEnabled(finalSelector)) throw new Error("final_review_missing: douyin_final_button_missing_or_ambiguous")
        status = "awaiting_review"
        errorCode = ""
      }
    } else if (target.platformCode === "kuaishou") {
      const description = target.body || target.caption
      const withTags = Array.isArray(target.tags) && target.tags.length
        ? description.replace(/\s+$/u, '') + "\n\n" + target.tags.map((tag) => "#" + tag).join(' ')
        : description
      if (target.media.length !== 1) {
        errorCode = target.media.length > 1 ? "EGO_PREPARE_MULTI_MEDIA_UNVERIFIED" : "EGO_PUBLISH_MEDIA_REQUIRED"
      } else {
        const inputSelector = "input[type='file']"
        await uploadFile(inputSelector, target.media[0].filePath)
        let fields = null
        let uploadReady = false
        for (let attempt = 0; attempt < 24 && remainingPublishBudget() > 1200; attempt += 1) {
          fields = await js("(() => {const visible=(node)=>{if(!node)return false;const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.display!=='none'&&s.visibility!=='hidden'&&r.width>0&&r.height>0};const title=[\"input[placeholder*='标题']\",\"textarea[placeholder*='标题']\"].find((selector)=>visible(document.querySelector(selector)))||'';const body=[\"textarea[placeholder*='描述']\",\"div[contenteditable='true']\"].find((selector)=>visible(document.querySelector(selector)))||'';const text=(document.body?.innerText||'').slice(0,8000);return {title,body,uploading:/上传中|正在上传|处理中|转码中|uploading|processing/i.test(text),login:Boolean(document.querySelector(\"input[type='password']\"))&&/登录|请先登录|sign in|log in/i.test(text) ? true : false}})()")
          if (fields?.login) {
            errorCode = "EGO_ACCOUNT_NOT_LOGGED_IN"
            break
          }
          uploadReady = await js("(() => {const visible=(node)=>{if(!node)return false;const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.display!=='none'&&s.visibility!=='hidden'&&r.width>0&&r.height>0};const text=(document.body?.innerText||'').slice(0,8000);const selected=[...document.querySelectorAll(\"input[type='file']\")].some((node)=>node.files?.length===1);const ready=[...document.querySelectorAll(\"button,[role='button']\")].some((node)=>visible(node)&&['重新上传','清空并重新上传'].includes((node.textContent||'').trim()));const preview=[...document.querySelectorAll('img,video')].some((node)=>{if(!visible(node))return false;const r=node.getBoundingClientRect(),src=String(node.currentSrc||node.src||'');return r.width>=64&&r.height>=72&&(node.tagName==='VIDEO'||/^(blob:|data:)/i.test(src))});return !/上传中|正在上传|处理中|转码中|uploading|processing/i.test(text)&&(selected||ready||preview)})()")
          if (fields?.title && fields?.body && uploadReady && !fields.uploading) break
          await new Promise((resolve) => setTimeout(resolve, 500))
        }
        if (errorCode === "EGO_ACCOUNT_NOT_LOGGED_IN") {
          preparationResults.push({ platformCode: target.platformCode, status, errorCode })
          continue
        }
        if (!uploadReady) {
          errorCode = "EGO_KUAISHOU_UPLOAD_TIMEOUT"
        } else if (!fields?.title || !fields?.body) {
          errorCode = "EGO_KUAISHOU_REQUIRED_CONTROL_MISSING"
        }
        if (errorCode !== "EGO_PREPARE_ADAPTER_UNVERIFIED") {
          preparationResults.push({ platformCode: target.platformCode, status, errorCode })
          continue
        }
        await fillInput(fields.title, target.title)
        if (fields.body.startsWith('textarea')) {
          await fillInput(fields.body, withTags)
        } else if (!await fillRichEditor(fields.body, withTags)) {
          throw new Error("body_fill_failed")
        }
        const readback = await js("(() => {const title=" + JSON.stringify(target.title) + ",body=" + JSON.stringify(withTags) + ";const titleNode=document.querySelector(\"input[placeholder*='标题'],textarea[placeholder*='标题']\");const bodyNode=document.querySelector(\"textarea[placeholder*='描述'],div[contenteditable='true']\");const actualTitle=titleNode?.value||'';const actualBody=(bodyNode?.value||bodyNode?.innerText||bodyNode?.textContent||'').replace(/\\r\\n?/g,'\\n');return actualTitle===title&&(actualBody===body||actualBody===body+'\\n')})()")
        if (!readback) throw new Error("kuaishou_readback_mismatch")
        const kuaishouExpectedSchedule = (publishAt) => {
          const instant = new Date(publishAt)
          if (Number.isNaN(instant.getTime()) || instant.getUTCSeconds() !== 0 || instant.getUTCMilliseconds() !== 0) return null
          const fields = Object.fromEntries(new Intl.DateTimeFormat('en-GB', {
            timeZone: 'Asia/Shanghai', calendar: 'gregory', year: 'numeric', month: '2-digit', day: '2-digit',
            hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
          }).formatToParts(instant).filter((part) => part.type !== 'literal').map((part) => [part.type, part.value]))
          const values = ['year', 'month', 'day', 'hour', 'minute'].map((name) => Number(fields[name]))
          if (!values.every(Number.isInteger)) return null
          const [year, month, day, hour, minute] = values
          if (month < 1 || month > 12 || day < 1 || day > 31 || hour < 0 || hour > 23 || minute < 0 || minute > 59) return null
          const pad = (value) => String(value).padStart(2, '0')
          const date = year + '-' + pad(month) + '-' + pad(day)
          const time = pad(hour) + ':' + pad(minute)
          return { date, time, dateTimeMinute: date + ' ' + time, dateTimeLocal: date + 'T' + time }
        }
        const uniqueKuaishouScheduleControl = async (labels, marker) => js("(() => {" +
          "const labels=" + JSON.stringify(labels) + ",marker=" + JSON.stringify(marker) + ";const attribute='data-alpha-kuaishou-schedule-'+marker;for(const node of document.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);" +
          "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
          "const normalize=(value)=>String(value||'').normalize('NFKC').replace(/\\s+/g,' ').trim();const wanted=new Set(labels.map(normalize));" +
          "const labelText=(node)=>{const aria=node.getAttribute('aria-label');if(aria)return normalize(aria);const labelledBy=node.getAttribute('aria-labelledby');if(labelledBy){const text=labelledBy.split(/\\s+/).map((id)=>document.getElementById(id)?.textContent||'').join(' ');if(normalize(text))return normalize(text)}if(node.labels?.length)return normalize([...node.labels].map((label)=>label.textContent||'').join(' '));const label=node.closest('label');if(label)return normalize(label.textContent||'');return normalize(node.innerText||node.textContent||'')};" +
          "const raw=[...document.querySelectorAll(\"input[type='radio'],input[type='checkbox'],[role='radio'],[role='checkbox'],label\")].filter((node)=>wanted.has(labelText(node))&&(visible(node)||visible(node.closest('label'))));const controls=raw.filter((node)=>!raw.some((other)=>other!==node&&other.contains(node)));if(controls.length!==1)return controls.length>1?'__ambiguous__':'';controls[0].setAttribute(attribute,'true');return '['+attribute+'=\\\"true\\\"]';})()")
        const kuaishouScheduleControlChecked = async (selector) => js("(() => {const node=document.querySelector(" + JSON.stringify(selector) + ");if(!node)return null;const stateSelector=\"input[type='radio'],input[type='checkbox'],[role='radio'],[role='checkbox'],[aria-checked]\";const state=(control)=>{if(!control)return null;if(control.checked===true||control.checked===false)return control.checked;for(const name of ['aria-checked','aria-selected','data-selected']){const value=control.getAttribute(name);if(value==='true'||value==='false')return value==='true'}return control.hasAttribute('checked')||control.hasAttribute('selected')||control.classList.contains('selected')||control.classList.contains('active')||control.classList.contains('checked')};if(node.matches(stateSelector))return state(node);if(node.tagName==='LABEL'&&node.htmlFor){const linked=document.getElementById(node.htmlFor);if(linked)return state(linked)}const nested=[...node.querySelectorAll(stateSelector)];if(nested.length===1)return state(nested[0]);let scope=node.parentElement;for(let depth=0;scope&&depth<5;depth+=1,scope=scope.parentElement){const controls=[...scope.querySelectorAll(stateSelector)];if(controls.length===1)return state(controls[0])}return null;})()")
        const uniqueKuaishouScheduleInput = async (marker) => js("(() => {const marker=" + JSON.stringify(marker) + ";const attribute='data-alpha-kuaishou-schedule-'+marker;for(const node of document.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const selectors=[\"input[placeholder='日期和时间'][format='yyyy-MM-dd HH:mm']\",\".ant-picker-input input\",\"input[type='datetime-local']\"];const nodes=[...new Set(selectors.flatMap((selector)=>[...document.querySelectorAll(selector)]))].filter(visible);if(nodes.length!==1)return nodes.length>1?'__ambiguous__':'';nodes[0].setAttribute(attribute,'true');return '['+attribute+'=\\\"true\\\"]';})()")
        const kuaishouScheduleReadbackStable = async (selector, expected) => {
          const matches = (value) => {
            const actual = String(value || '').replace(/\\s+/g, ' ').trim()
            return actual === expected.dateTimeMinute || actual.startsWith(expected.dateTimeMinute + ':') || actual === expected.dateTimeLocal
          }
          let previous = ''
          for (let attempt = 0; attempt < 2; attempt += 1) {
            const readback = await js("(() => {const node=document.querySelector(" + JSON.stringify(selector) + ");return node?.value||node?.getAttribute('value')||node?.innerText||node?.textContent||''})()")
            if (matches(readback)) {
              const signature = String(readback).replace(/\\s+/g, ' ').trim()
              if (signature === previous) return true
              previous = signature
            }
            await new Promise((resolve) => setTimeout(resolve, 350))
          }
          return false
        }
        const chooseKuaishouDateTimePicker = async (inputSelector, expected) => {
          const visiblePicker = async () => js("(() => {const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const pickers=[...document.querySelectorAll('.ant-picker-dropdown')].filter(visible);if(pickers.length!==1)return pickers.length>1?'__ambiguous__':'';pickers[0].setAttribute('data-alpha-kuaishou-picker','true');return '[data-alpha-kuaishou-picker=\\\"true\\\"]'})()")
          let pickerSelector = await visiblePicker()
          if (!pickerSelector) {
            await click(inputSelector, { label: "open Kuaishou date time picker" })
            await new Promise((resolve) => setTimeout(resolve, 150))
            pickerSelector = await visiblePicker()
          }
          if (pickerSelector === '__ambiguous__' || !pickerSelector) return false
          const daySelector = await js("(() => {const picker=document.querySelector('[data-alpha-kuaishou-picker=\\\"true\\\"]'),expected=" + JSON.stringify(expected.date) + ";if(!picker)return '';const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const cells=[...picker.querySelectorAll('.ant-picker-cell[title]')].filter((node)=>visible(node)&&!node.classList.contains('ant-picker-cell-disabled')&&node.getAttribute('aria-disabled')!=='true'&&node.getAttribute('title')===expected);if(cells.length!==1)return cells.length>1?'__ambiguous__':'';cells[0].setAttribute('data-alpha-kuaishou-day','true');return '[data-alpha-kuaishou-day=\\\"true\\\"]'})()")
          if (daySelector === '__ambiguous__' || !daySelector) return false
          await click(daySelector, { label: "select Kuaishou schedule date" })
          const selectTime = async (columnIndex, wanted, marker) => {
            const optionSelector = await js("(() => {const picker=document.querySelector('[data-alpha-kuaishou-picker=\\\"true\\\"]'),wanted=" + JSON.stringify(wanted) + ",index=" + String(columnIndex) + ",marker=" + JSON.stringify(marker) + ";if(!picker)return '';const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const columns=[...picker.querySelectorAll('.ant-picker-time-panel-column')].filter(visible);if(!columns[index])return '';const items=[...columns[index].querySelectorAll('li')].filter((node)=>visible(node)&&(node.innerText||node.textContent||'').trim()===wanted);if(items.length!==1)return items.length>1?'__ambiguous__':'';items[0].setAttribute('data-alpha-kuaishou-time-'+marker,'true');return '[data-alpha-kuaishou-time-'+marker+'=\\\"true\\\"]'})()")
            if (optionSelector === '__ambiguous__' || !optionSelector) return false
            await click(optionSelector, { label: "select Kuaishou schedule time" })
            return true
          }
          const columnsReady = await js("(() => {const picker=document.querySelector('[data-alpha-kuaishou-picker=\\\"true\\\"]');return Boolean(picker&&picker.querySelectorAll('.ant-picker-time-panel-column').length>=2)})()")
          if (!columnsReady) return false
          if (!await selectTime(0, String(Number(expected.time.slice(0, 2))), 'hour') || !await selectTime(1, String(Number(expected.time.slice(3, 5))), 'minute')) return false
          const confirmSelector = await js("(() => {const picker=document.querySelector('[data-alpha-kuaishou-picker=\\\"true\\\"]');if(!picker)return '';const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const buttons=[...picker.querySelectorAll('button,[role=\\\"button\\\"]')].filter((node)=>visible(node)&&(node.innerText||node.textContent||'').trim()==='确定');if(buttons.length!==1)return buttons.length>1?'__ambiguous__':'';buttons[0].setAttribute('data-alpha-kuaishou-confirm','true');return '[data-alpha-kuaishou-confirm=\\\"true\\\"]'})()")
          if (confirmSelector === '__ambiguous__' || !confirmSelector) return false
          await click(confirmSelector, { label: "confirm Kuaishou schedule" })
          const closed = await js("(() => {const picker=document.querySelector('[data-alpha-kuaishou-picker=\\\"true\\\"]');if(!picker)return true;const style=getComputedStyle(picker),rect=picker.getBoundingClientRect();return style.display==='none'||style.visibility==='hidden'||rect.width===0||rect.height===0})()")
          return Boolean(closed) && await kuaishouScheduleReadbackStable(inputSelector, expected)
        }
        if (target.publishAt) {
          prepareStage = "schedule_mode"
          const expectedSchedule = kuaishouExpectedSchedule(target.publishAt)
          if (!expectedSchedule || new Date(target.publishAt).getTime() <= Date.now()) throw new Error("kuaishou_schedule_time_invalid")
          const scheduleSelector = await uniqueKuaishouScheduleControl(["定时发布"], "schedule-mode")
          if (scheduleSelector === "__ambiguous__" || !scheduleSelector) throw new Error("kuaishou_schedule_mode_missing_or_ambiguous")
          if (await kuaishouScheduleControlChecked(scheduleSelector) !== true) {
            await click(scheduleSelector, { label: "select Kuaishou schedule mode" })
            await new Promise((resolve) => setTimeout(resolve, 300))
          }
          if (await kuaishouScheduleControlChecked(scheduleSelector) !== true) throw new Error("kuaishou_schedule_mode_readback_mismatch")
          prepareStage = "schedule_time"
          const inputSelector = await uniqueKuaishouScheduleInput("schedule-input")
          if (inputSelector === "__ambiguous__" || !inputSelector) throw new Error("kuaishou_schedule_picker_missing_or_ambiguous")
          const inputType = await js("(() => document.querySelector(" + JSON.stringify(inputSelector) + ")?.type || '')()")
          let scheduleApplied = false
          if (inputType === "datetime-local") {
            await fillInput(inputSelector, expectedSchedule.dateTimeLocal)
            scheduleApplied = await kuaishouScheduleReadbackStable(inputSelector, expectedSchedule)
          } else {
            scheduleApplied = await chooseKuaishouDateTimePicker(inputSelector, expectedSchedule)
          }
          if (!scheduleApplied) throw new Error("kuaishou_schedule_time_readback_mismatch")
          const immediateSelector = await uniqueKuaishouScheduleControl(["立即发布"], "immediate-mode")
          if (!immediateSelector || await kuaishouScheduleControlChecked(immediateSelector) !== false) throw new Error("kuaishou_schedule_mode_readback_mismatch")
        }
        const finalState = await js("(() => {const visible=(node)=>{if(!node)return false;const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.display!=='none'&&s.visibility!=='hidden'&&r.width>0&&r.height>0};const controls=[...document.querySelectorAll(\"button,[role='button'],[tabindex='0'],a,[class*='button'],[class*='btn']\")].filter((node)=>visible(node)&&(node.textContent||'').trim()==='发布');return {count:controls.length,enabled:controls.length===1&&!controls[0].disabled&&controls[0].getAttribute('aria-disabled')!=='true'}})()")
        if (!finalState || finalState.count !== 1) throw new Error("kuaishou_schedule_final_button_missing_or_disabled")
        if (!finalState.enabled) throw new Error("kuaishou_schedule_final_button_missing_or_disabled")
        status = "awaiting_review"
        errorCode = ""
      }
    }
  } catch (error) {
    if (/platform_verification_required/i.test(String(error?.message || error))) {
      errorCode = "LOGIN_REQUIRED"
	} else if (/publish_composer_tab_limit/i.test(String(error?.message || error))) {
	  errorCode = "EGO_PUBLISH_COMPOSER_TAB_LIMIT"
    } else if (!errorCode || errorCode === "EGO_PREPARE_ADAPTER_UNVERIFIED") {
      errorCode = reviewedPrepareErrorCode(target.platformCode, error)
      if (target.platformCode === "douyin" && errorCode === "EGO_PUBLISH_PREPARE_REVIEW_REQUIRED") {
        const stageCodes = {
          composer_open: "EGO_DOUYIN_COMPOSER_OPEN_FAILED",
          entry: "EGO_DOUYIN_ENTRY_FLOW_FAILED",
          home_route: "EGO_DOUYIN_HOME_ROUTE_UNVERIFIED",
          entry_or_fallback: "EGO_DOUYIN_ENTRY_OR_FALLBACK_FAILED",
          image_input: "EGO_DOUYIN_IMAGE_INPUT_MISSING",
		  image_input_multiple: "EGO_DOUYIN_IMAGE_INPUT_MULTIPLE_UNSUPPORTED",
          upload: "EGO_DOUYIN_UPLOAD_FAILED",
		  upload_readback: "EGO_DOUYIN_IMAGE_UPLOAD_READBACK_MISMATCH",
          required_image_route: "EGO_DOUYIN_REQUIRED_IMAGE_ROUTE_MISSING",
		  upload_complete: "EGO_DOUYIN_UPLOAD_IN_PROGRESS",
          controls: "EGO_DOUYIN_REQUIRED_CONTROL_MISSING",
          title: "EGO_DOUYIN_TITLE_READBACK_MISMATCH",
          body: "EGO_DOUYIN_BODY_READBACK_MISMATCH",
          topics: "EGO_DOUYIN_TOPIC_READBACK_MISMATCH",
          schedule_mode: "EGO_DOUYIN_SCHEDULE_MODE_READBACK_MISMATCH",
          schedule_time: "EGO_DOUYIN_SCHEDULE_TIME_READBACK_MISMATCH",
          final_control: "EGO_DOUYIN_FINAL_CONTROL_MISSING"
        }
        errorCode = stageCodes[prepareStage] || errorCode
      }
    }
    if (target.platformCode === "douyin" && /^EGO_DOUYIN_TOPIC_/.test(errorCode)) {
      try {
        diagnostic = await boundedDouyinTopicDiagnostic(douyinDiagnosticBodySelector)
      } catch {}
    }
  }
  preparationResults.push({ platformCode: target.platformCode, status, errorCode, diagnostic })
}
cliLog(%q + JSON.stringify({
  done: true,
  results: preparationResults,
}))
`, spaceID, jsErrorLiteral(ErrSpaceBindingMissing), jsErrorLiteral(ErrSpaceUserControlled), spaceID, spaceID, targetsJSON, resultPrefix)
}

func buildEgoPublishSubmitProgram(spaceID int64, targets []publishRuntimeTarget) string {
	targetsJSON, _ := json.Marshal(publishRuntimeTargetsForExecution(targets))
	return fmt.Sprintf(`const spaces = await listTaskSpaces()
const existingSpace = spaces.find((item) => Number(item.id) === %d)
if (!existingSpace) throw new Error(%s)
if (existingSpace.ownership === "agentDelegatedToUser" || existingSpace.ownership === "user") {
  throw new Error(%s)
}
await useOrCreateTaskSpace(%d)
const publishTargets = %s
`+platformChallengeContractJS+douyinReadbackContractJS+wechatVideoScheduleContractJS+`
const submitResults = []
const alphaInteractionPause = async (milliseconds, minimum, maximum) => {
  const pause = Number(milliseconds)
  if (!Number.isInteger(pause) || pause < minimum || pause > maximum) throw new Error("publish_interaction_pacing_invalid")
  await new Promise((resolve) => setTimeout(resolve, pause))
}
const visible = (element) => {
  if (!element) return false
  const style = getComputedStyle(element), rect = element.getBoundingClientRect()
  return style.visibility !== "hidden" && style.display !== "none" && rect.width > 0 && rect.height > 0
}
const uniqueDouyinScheduleControl = async (labels, marker) => js("(() => {" +
  "const labels=" + JSON.stringify(labels) + ",marker=" + JSON.stringify(marker) + ";" +
  "const attribute='data-alpha-douyin-submit-'+marker;for(const node of document.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);" +
  "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
  "const normalize=(value)=>String(value||'').normalize('NFKC').replace(/\\s+/g,' ').trim();const wanted=new Set(labels.map(normalize));" +
  "const controlLabel=(node)=>{const aria=node.getAttribute('aria-label');if(aria)return normalize(aria);const labelledBy=node.getAttribute('aria-labelledby');if(labelledBy){const text=labelledBy.split(/\\s+/).map((id)=>document.getElementById(id)?.textContent||'').join(' ');if(normalize(text))return normalize(text)}if(node.labels?.length)return normalize([...node.labels].map((label)=>label.textContent||'').join(' '));const label=node.closest('label');if(label)return normalize(label.textContent||'');return normalize(node.innerText||node.textContent||'')};" +
  "const controls=[...document.querySelectorAll(\"input[type='radio'],input[type='checkbox'],[role='radio'],[role='checkbox']\")].filter((node)=>wanted.has(controlLabel(node))&&(visible(node)||[...(node.labels||[])].some(visible)||visible(node.closest('label'))));" +
  "if(controls.length!==1)return '';controls[0].setAttribute(attribute,'true');return '['+attribute+'=\\\"true\\\"]';})()")
const douyinScheduleControlChecked = async (selector) => js("(() => {const node=document.querySelector(" + JSON.stringify(selector) + ");if(!node)return null;const stateSelector=\"input[type='radio'],input[type='checkbox'],[role='radio'],[role='checkbox'],[aria-checked]\";const state=(control)=>{if(!control)return null;if(control.checked===true||control.checked===false)return control.checked;for(const name of ['aria-checked','aria-selected','data-selected']){const value=control.getAttribute(name);if(value==='true'||value==='false')return value==='true'}return control.hasAttribute('checked')||control.hasAttribute('selected')||control.classList.contains('selected')||control.classList.contains('active')||control.classList.contains('checked')};if(node.matches(stateSelector))return state(node);if(node.tagName==='LABEL'&&node.htmlFor){const linked=document.getElementById(node.htmlFor);if(linked)return state(linked)}const nested=[...node.querySelectorAll(stateSelector)];if(nested.length===1)return state(nested[0]);let scope=node.parentElement;for(let depth=0;scope&&depth<5;depth+=1,scope=scope.parentElement){const controls=[...scope.querySelectorAll(stateSelector)];if(controls.length===1)return state(controls[0])}return null;})()")
const uniqueDouyinScheduleTimeInput = async () => js("(() => {const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const inputs=[...document.querySelectorAll(\"input[type='text'][placeholder='日期和时间'][format='yyyy-MM-dd HH:mm']\")].filter(visible);if(inputs.length!==1)return '';inputs[0].setAttribute('data-alpha-douyin-submit-time','true');return '[data-alpha-douyin-submit-time=\\\"true\\\"]';})()")
const douyinExpectedSchedule = (publishAt) => { const date = new Date(publishAt); if (Number.isNaN(date.getTime()) || date.getSeconds() !== 0 || date.getMilliseconds() !== 0) return ''; const pad = (value) => String(value).padStart(2, '0'); return date.getFullYear() + '-' + pad(date.getMonth() + 1) + '-' + pad(date.getDate()) + ' ' + pad(date.getHours()) + ':' + pad(date.getMinutes()) }
const uniqueDouyinFinalButton = async () => js("(() => {const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const buttons=[...document.querySelectorAll('button')].filter((node)=>visible(node)&&(node.innerText||node.textContent||'').trim()==='发布'&&!node.disabled&&node.getAttribute('aria-disabled')!=='true');if(buttons.length!==1)return '';buttons[0].setAttribute('data-alpha-douyin-submit-final','true');return '[data-alpha-douyin-submit-final=\\\"true\\\"]';})()")
const normalizeYouTubeScheduleText = (value) => String(value || '').normalize('NFKC').replace(/\s+/g, ' ').trim()
const normalizeYouTubeDescription = (value) => String(value || '').replace(/\r\n?/g, '\n')
const youtubeExpectedSchedule = (publishAt) => {
  const instant = new Date(publishAt)
  if (Number.isNaN(instant.getTime()) || instant.getUTCSeconds() !== 0 || instant.getUTCMilliseconds() !== 0) return null
  const fields = Object.fromEntries(new Intl.DateTimeFormat('en-GB', {
    timeZone: 'Asia/Shanghai', calendar: 'gregory', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
  }).formatToParts(instant).filter((part) => part.type !== 'literal').map((part) => [part.type, part.value]))
  const year = Number(fields.year), month = Number(fields.month), day = Number(fields.day), hour = Number(fields.hour), minute = Number(fields.minute)
  if (![year, month, day, hour, minute].every(Number.isInteger) || month < 1 || month > 12 || day < 1 || day > 31 || hour < 0 || hour > 23 || minute < 0 || minute > 59) return null
  const pad = (value) => String(value).padStart(2, '0'), months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
  const period = hour >= 12 ? 'PM' : 'AM', twelveHour = hour %% 12 || 12, timeLabel = twelveHour + ':' + pad(minute) + '\u202f' + period
  return { dateLabels: [months[month - 1] + ' ' + day + ', ' + year, year + '年' + month + '月' + day + '日'], timeLabels: [timeLabel, timeLabel.replace('\u202f', ' ')] }
}
const uniqueYouTubeSelector = async (selectors, marker) => js("(() => {" +
  "const selectors=" + JSON.stringify(selectors) + ",marker=" + JSON.stringify(marker) + ";const attribute='data-alpha-youtube-'+marker;for(const node of document.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);" +
  "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const nodes=[...new Set(selectors.flatMap((selector)=>[...document.querySelectorAll(selector)]))].filter(visible);if(nodes.length!==1)return '';nodes[0].setAttribute(attribute,'true');return '['+attribute+'=\\\"true\\\"]';})()")
const uniqueYouTubeScheduleOption = async (marker) => js("(() => {" +
  "const marker=" + JSON.stringify(marker) + ";const attribute='data-alpha-youtube-'+marker;for(const node of document.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);" +
  "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const normalize=(value)=>String(value||'').normalize('NFKC').replace(/\\s+/g,' ').trim();const roots=[...document.querySelectorAll('ytcp-visibility-scheduler')].filter(visible);if(roots.length!==1)return '';const raw=[...roots[0].querySelectorAll(\"tp-yt-paper-radio-button,[role='radio'],label\")].filter(visible).filter((node)=>['Schedule','定时发布'].includes(normalize(node.innerText||node.textContent||node.getAttribute('aria-label')||'')));const controls=raw.filter((node)=>!raw.some((other)=>other!==node&&other.contains(node)));if(controls.length!==1)return '';controls[0].setAttribute(attribute,'true');return '['+attribute+'=\\\"true\\\"]';})()")
const youtubeScheduleSelected = async () => js("(() => {" +
  "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const normalize=(value)=>String(value||'').normalize('NFKC').replace(/\\s+/g,' ').trim();const roots=[...document.querySelectorAll('ytcp-visibility-scheduler')].filter(visible);if(roots.length!==1)return false;const raw=[...roots[0].querySelectorAll(\"tp-yt-paper-radio-button,[role='radio'],label\")].filter(visible).filter((node)=>['Schedule','定时发布'].includes(normalize(node.innerText||node.textContent||node.getAttribute('aria-label')||'')));const controls=raw.filter((node)=>!raw.some((other)=>other!==node&&other.contains(node)));if(controls.length!==1)return false;" +
  "const state=(node)=>{if(node.checked===true)return true;if(node.checked===false)return false;for(const name of ['aria-checked','aria-selected','data-selected']){const value=node.getAttribute(name);if(value==='true')return true;if(value==='false')return false}if(node.hasAttribute('checked')||node.hasAttribute('selected')||node.classList.contains('iron-selected')||node.classList.contains('selected')||node.classList.contains('active'))return true;const nested=[...node.querySelectorAll(\"input[type='radio'],input[type='checkbox'],[role='radio'],[role='checkbox'],[aria-checked]\")];return nested.length===1?state(nested[0]):false};return state(controls[0]);})()")
const youtubeScheduleReadbackStable = async (dateSelector, timeSelector, expected) => {
  let previous = ''
  for (let attempt = 0; attempt < 2; attempt += 1) {
    const readback = await js("(() => {const date=document.querySelector(" + JSON.stringify(dateSelector) + "),time=document.querySelector(" + JSON.stringify(timeSelector) + ");return {date:date?.innerText||date?.textContent||'',time:time?.value||time?.getAttribute('value')||''}})()")
    const dateText = normalizeYouTubeScheduleText(readback?.date), timeText = normalizeYouTubeScheduleText(readback?.time)
    if (expected.dateLabels.some((label) => normalizeYouTubeScheduleText(label) === dateText) && expected.timeLabels.some((label) => normalizeYouTubeScheduleText(label) === timeText) && await youtubeScheduleSelected()) {
      const signature = dateText + '|' + timeText
      if (signature === previous) return true
      previous = signature
    }
    await new Promise((resolve) => setTimeout(resolve, 350))
  }
  return false
}
  const uniqueYouTubeScheduleFinalButton = async () => js("(() => {for(const node of document.querySelectorAll('[data-alpha-youtube-submit-schedule-final]'))node.removeAttribute('data-alpha-youtube-submit-schedule-final');const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const buttons=[...document.querySelectorAll(\"button,[role='button'],tp-yt-paper-button\")].filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&['Schedule','定时发布'].includes((node.innerText||node.textContent||'').trim()));if(buttons.length!==1)return '';buttons[0].setAttribute('data-alpha-youtube-submit-schedule-final','true');return '[data-alpha-youtube-submit-schedule-final=\\\"true\\\"]';})()")
const tiktokExpectedSchedule = (publishAt) => {
  const instant = new Date(publishAt)
  if (Number.isNaN(instant.getTime())) return null
  const fields = Object.fromEntries(new Intl.DateTimeFormat('en-GB', {
    timeZone: 'Asia/Shanghai', calendar: 'gregory', year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
  }).formatToParts(instant).filter((part) => part.type !== 'literal').map((part) => [part.type, part.value]))
  const year = Number(fields.year), month = Number(fields.month), day = Number(fields.day)
  const hour = Number(fields.hour), minute = Number(fields.minute)
  if (![year, month, day, hour, minute].every(Number.isInteger) || month < 1 || month > 12 || day < 1 || day > 31 || hour < 0 || hour > 23 || minute < 0 || minute > 59) return null
  const rounded = Math.ceil((hour * 60 + minute) / 5) * 5
  const dateParts = (value) => Object.fromEntries(new Intl.DateTimeFormat('en-GB', {
    timeZone: 'Asia/Shanghai', calendar: 'gregory', year: 'numeric', month: '2-digit', day: '2-digit',
  }).formatToParts(value).filter((part) => part.type !== 'literal').map((part) => [part.type, part.value]))
  const next = rounded >= 1440 ? dateParts(new Date(Date.UTC(year, month - 1, day) + 86400000)) : { year: String(year), month: String(month).padStart(2, '0'), day: String(day).padStart(2, '0') }
  const nextHour = rounded >= 1440 ? 0 : Math.floor(rounded / 60)
  const nextMinute = rounded >= 1440 ? 0 : rounded %% 60
  const date = String(next.year).padStart(4, '0') + '-' + String(next.month).padStart(2, '0') + '-' + String(next.day).padStart(2, '0')
  const time = String(nextHour).padStart(2, '0') + ':' + String(nextMinute).padStart(2, '0')
  return { date, time }
}
const uniqueTikTokSubmitSelector = async (selectors, marker) => js("(() => {" +
  "const selectors=" + JSON.stringify(selectors) + ",marker=" + JSON.stringify(marker) + ";const attribute='data-alpha-tiktok-submit-'+marker;for(const node of document.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);" +
  "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const nodes=[...new Set(selectors.flatMap((selector)=>[...document.querySelectorAll(selector)]))].filter(visible);if(nodes.length!==1)return nodes.length>1?'__ambiguous__':'';nodes[0].setAttribute(attribute,'true');return '['+attribute+'=\\\"true\\\"]';})()")
const uniqueTikTokSubmitScheduleControl = async () => js("(() => {" +
  "const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const labels=new Set(['Schedule','定时发布','预约发布']);const raw=[...document.querySelectorAll(\"input[type='radio'],[role='radio'],label\")].filter((node)=>visible(node)&&labels.has((node.innerText||node.textContent||node.getAttribute('aria-label')||'').normalize('NFKC').replace(/\\s+/g,' ').trim()));const controls=raw.filter((node)=>!raw.some((other)=>other!==node&&other.contains(node)));if(controls.length!==1)return controls.length>1?'__ambiguous__':'';controls[0].setAttribute('data-alpha-tiktok-submit-schedule','true');return '[data-alpha-tiktok-submit-schedule=\\\"true\\\"]';})()")
const readTikTokSubmitBoolean = async (selector) => js("(() => {const node=document.querySelector(" + JSON.stringify(selector) + ");if(!node)return null;if(node.checked===true||node.checked===false)return node.checked;const state=node.querySelector('input[type=\\\"radio\\\"],input[type=\\\"checkbox\\\"],[role=\\\"radio\\\"],[role=\\\"checkbox\\\"],[aria-checked]');if(!state)return null;if(state.checked===true||state.checked===false)return state.checked;const value=state.getAttribute('aria-checked')||state.getAttribute('aria-selected')||state.getAttribute('data-selected');return value==='true'?true:value==='false'?false:null;})()")
const tiktokSubmitScheduleInputs = async () => js("(() => {const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const inputs=[...document.querySelectorAll('.scheduled-picker input.TUXTextInputCore-input')].filter(visible);if(inputs.length!==2)return inputs.length>2?'__ambiguous__':'';let date='',time='';for(const node of inputs){const value=String(node.value||'');if(/^\\d{4}-\\d{2}-\\d{2}$/.test(value))date=node;else if(/^\\d{2}:\\d{2}$/.test(value))time=node;}if(!date||!time)return '';date.setAttribute('data-alpha-tiktok-submit-date','true');time.setAttribute('data-alpha-tiktok-submit-time','true');return {dateSelector:'[data-alpha-tiktok-submit-date=\\\"true\\\"]',timeSelector:'[data-alpha-tiktok-submit-time=\\\"true\\\"]'};})()")
const tiktokSubmitScheduleReadbackStable = async (selectors, expected) => {
  let previous = ''
  for (let attempt = 0; attempt < 2; attempt += 1) {
    const readback = await js("(() => {const date=document.querySelector(" + JSON.stringify(selectors?.dateSelector || '') + "),time=document.querySelector(" + JSON.stringify(selectors?.timeSelector || '') + ");return {date:date?.value||'',time:time?.value||''}})()")
    if (readback?.date === expected.date && readback?.time === expected.time) {
      const signature = readback.date + '|' + readback.time
      if (signature === previous) return true
      previous = signature
    }
    await new Promise((resolve) => setTimeout(resolve, 350))
  }
  return false
}
const uniqueTikTokSubmitFinalButton = async () => js("(() => {const visible=(node)=>{const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const labels=new Set(['Schedule','定时发布','预约发布']);const buttons=[...document.querySelectorAll('button,[role=\\\"button\\\"],input[type=\\\"submit\\\"]')].filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&labels.has((node.innerText||node.textContent||node.value||'').trim()));if(buttons.length!==1)return buttons.length>1?'__ambiguous__':'';buttons[0].setAttribute('data-alpha-tiktok-submit-final','true');return '[data-alpha-tiktok-submit-final=\\\"true\\\"]';})()")
const kuaishouExpectedSchedule = (publishAt) => {
  const instant = new Date(publishAt)
  if (Number.isNaN(instant.getTime()) || instant.getUTCSeconds() !== 0 || instant.getUTCMilliseconds() !== 0) return null
  const fields = Object.fromEntries(new Intl.DateTimeFormat('en-GB', {
    timeZone: 'Asia/Shanghai', calendar: 'gregory', year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
  }).formatToParts(instant).filter((part) => part.type !== 'literal').map((part) => [part.type, part.value]))
  const values = ['year', 'month', 'day', 'hour', 'minute'].map((name) => Number(fields[name]))
  if (!values.every(Number.isInteger)) return null
  const [year, month, day, hour, minute] = values
  if (month < 1 || month > 12 || day < 1 || day > 31 || hour < 0 || hour > 23 || minute < 0 || minute > 59) return null
  const pad = (value) => String(value).padStart(2, '0')
  const date = year + '-' + pad(month) + '-' + pad(day)
  const time = pad(hour) + ':' + pad(minute)
  return { date, time, dateTimeMinute: date + ' ' + time, dateTimeLocal: date + 'T' + time }
}
const uniqueKuaishouSubmitSelector = async (selectors, marker) => js("(() => {const selectors=" + JSON.stringify(selectors) + ",marker=" + JSON.stringify(marker) + ";const attribute='data-alpha-kuaishou-submit-'+marker;for(const node of document.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const nodes=[...new Set(selectors.flatMap((selector)=>[...document.querySelectorAll(selector)]))].filter(visible);if(nodes.length!==1)return nodes.length>1?'__ambiguous__':'';nodes[0].setAttribute(attribute,'true');return '['+attribute+'=\\\"true\\\"]';})()")
const uniqueKuaishouSubmitScheduleControl = async (labels, marker) => js("(() => {const labels=" + JSON.stringify(labels) + ",marker=" + JSON.stringify(marker) + ";const attribute='data-alpha-kuaishou-submit-'+marker;for(const node of document.querySelectorAll('['+attribute+']'))node.removeAttribute(attribute);const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const normalize=(value)=>String(value||'').normalize('NFKC').replace(/\\s+/g,' ').trim();const wanted=new Set(labels.map(normalize));const labelText=(node)=>{const aria=node.getAttribute('aria-label');if(aria)return normalize(aria);const labelledBy=node.getAttribute('aria-labelledby');if(labelledBy){const text=labelledBy.split(/\\s+/).map((id)=>document.getElementById(id)?.textContent||'').join(' ');if(normalize(text))return normalize(text)}if(node.labels?.length)return normalize([...node.labels].map((label)=>label.textContent||'').join(' '));const label=node.closest('label');if(label)return normalize(label.textContent||'');return normalize(node.innerText||node.textContent||'')};const raw=[...document.querySelectorAll(\"input[type='radio'],input[type='checkbox'],[role='radio'],[role='checkbox'],label\")].filter((node)=>wanted.has(labelText(node))&&(visible(node)||visible(node.closest('label'))));const controls=raw.filter((node)=>!raw.some((other)=>other!==node&&other.contains(node)));if(controls.length!==1)return controls.length>1?'__ambiguous__':'';controls[0].setAttribute(attribute,'true');return '['+attribute+'=\\\"true\\\"]';})()")
const readKuaishouSubmitBoolean = async (selector) => js("(() => {const node=document.querySelector(" + JSON.stringify(selector) + ");if(!node)return null;const stateSelector=\"input[type='radio'],input[type='checkbox'],[role='radio'],[role='checkbox'],[aria-checked]\";const state=(control)=>{if(!control)return null;if(control.checked===true||control.checked===false)return control.checked;for(const name of ['aria-checked','aria-selected','data-selected']){const value=control.getAttribute(name);if(value==='true'||value==='false')return value==='true'}return control.hasAttribute('checked')||control.hasAttribute('selected')||control.classList.contains('selected')||control.classList.contains('active')||control.classList.contains('checked')};if(node.matches(stateSelector))return state(node);if(node.tagName==='LABEL'&&node.htmlFor){const linked=document.getElementById(node.htmlFor);if(linked)return state(linked)}const nested=[...node.querySelectorAll(stateSelector)];if(nested.length===1)return state(nested[0]);let scope=node.parentElement;for(let depth=0;scope&&depth<5;depth+=1,scope=scope.parentElement){const controls=[...scope.querySelectorAll(stateSelector)];if(controls.length===1)return state(controls[0])}return null;})()")
const uniqueKuaishouSubmitScheduleInput = async () => js("(() => {const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const selectors=[\"input[placeholder='日期和时间'][format='yyyy-MM-dd HH:mm']\",\".ant-picker-input input\",\"input[type='datetime-local']\"];const nodes=[...new Set(selectors.flatMap((selector)=>[...document.querySelectorAll(selector)]))].filter(visible);if(nodes.length!==1)return nodes.length>1?'__ambiguous__':'';nodes[0].setAttribute('data-alpha-kuaishou-submit-schedule-input','true');return '[data-alpha-kuaishou-submit-schedule-input=\\\"true\\\"]';})()")
const kuaishouScheduleReadbackStable = async (selector, expected) => {
  const matches = (value) => {
    const actual = String(value || '').replace(/\\s+/g, ' ').trim()
    return actual === expected.dateTimeMinute || actual.startsWith(expected.dateTimeMinute + ':') || actual === expected.dateTimeLocal
  }
  let previous = ''
  for (let attempt = 0; attempt < 2; attempt += 1) {
    const readback = await js("(() => {const node=document.querySelector(" + JSON.stringify(selector) + ");return node?.value||node?.getAttribute('value')||node?.innerText||node?.textContent||''})()")
    if (matches(readback)) {
      const signature = String(readback).replace(/\\s+/g, ' ').trim()
      if (signature === previous) return true
      previous = signature
    }
    await new Promise((resolve) => setTimeout(resolve, 350))
  }
  return false
}
const uniqueKuaishouSubmitFinalButton = async () => js("(() => {const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};const buttons=[...document.querySelectorAll(\"button,[role='button'],[tabindex='0'],a,[class*='button'],[class*='btn']\")].filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&(node.innerText||node.textContent||'').trim()==='发布');if(buttons.length!==1)return buttons.length>1?'__ambiguous__':'';buttons[0].setAttribute('data-alpha-kuaishou-submit-final','true');return '[data-alpha-kuaishou-submit-final=\\\"true\\\"]';})()")
for (const target of publishTargets) {
  let status = "failed", errorCode = "EGO_SUBMIT_FORM_REVIEW_REQUIRED", clicked = false
  let submitStage = "composer"
  try {
    const expectedHost = new URL(target.composerURL).hostname
    const tabs = (await listTabs()).filter((tab) => { try { return new URL(String(tab.url||'')).hostname === expectedHost } catch { return false } })
    const tab = tabs[tabs.length-1]
    if (!tab) throw new Error("composer_missing")
    await switchTab(tab.targetId || tab.id)
    if (await alphaPlatformChallengeVisible()) {
      throw new Error("platform_verification_required")
    }
    let verified = false, selector = ''
    if (target.platformCode === "facebook") {
      submitStage = "identity"
      if (!target.publishAt) throw new Error("facebook_immediate_submit_unsupported")
      const metaPageID = String(target.metaPageID || '').trim()
      const metaPageName = String(target.metaPageName || '').trim()
	      const metaIdentityReadback = await js('(() => {const expectedID=' + JSON.stringify(metaPageID) + ',expectedName=' + JSON.stringify(metaPageName) + ';const searchParams=new URLSearchParams(location.search);const visible=(node)=>{const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.visibility!=="hidden"&&s.display!=="none"&&r.width>0&&r.height>0};const normalize=(value)=>String(value||"").normalize("NFKC").replace(/\\s+/g," ").trim();const candidates=[...document.querySelectorAll("[role=combobox],[aria-selected=true],[aria-current=true],[data-page-id],[data-asset-id]")].filter(visible).filter((node)=>normalize(node.innerText||node.textContent||node.getAttribute("aria-label")||"")===normalize(expectedName));return location.hostname==="business.facebook.com"&&searchParams.get("asset_id")===expectedID&&candidates.length===1})()')
      if (!metaIdentityReadback) throw new Error("meta_submit_page_identity_readback_mismatch")
      submitStage = "content"
      const metaSubmitState = await js("(() => {const expected=" + JSON.stringify(target.caption || target.body) + ",expectedFiles=" + JSON.stringify(target.media.map((item)=>item.filePath.split(/[\\/]/).pop())) + ";const visible=(node)=>{const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.visibility!=='hidden'&&s.display!=='none'&&r.width>0&&r.height>0};const editors=[...document.querySelectorAll(\"[contenteditable='true'],textarea,[role='textbox']\")].filter(visible);const inputs=[...document.querySelectorAll(\"input[type='file']\")].filter((node)=>node.files?.length>0);const files=inputs.length===1?[...inputs[0].files].map((file)=>String(file.name||'')):[];const previews=[...document.querySelectorAll(\"[data-testid*='preview' i] img,[data-testid*='media' i] img,[class*='preview' i] img,[class*='upload' i] img,[data-testid*='preview' i] video,[class*='preview' i] video,canvas\")].filter(visible).filter((node)=>{const r=node.getBoundingClientRect();return r.width>=64&&r.height>=64});const actual=editors.length===1?String(editors[0].value||editors[0].innerText||editors[0].textContent||'').replace(/\\r\\n?/g,'\\n'):'';const caption=actual===expected||actual===expected+'\\n';const media=files.length===expectedFiles.length&&files.every((name,index)=>name===expectedFiles[index])&&previews.length===expectedFiles.length;return {caption,media,editorCount:editors.length,inputCount:inputs.length,previewCount:previews.length}})()")
      if (!metaSubmitState?.caption || metaSubmitState.editorCount !== 1) throw new Error("meta_submit_caption_readback_mismatch")
      if (!metaSubmitState?.media || metaSubmitState.inputCount !== 1) throw new Error("meta_submit_media_readback_mismatch")
      submitStage = "schedule"
      const metaExpectedSchedule = (() => {
        const instant = new Date(target.publishAt)
        if (Number.isNaN(instant.getTime()) || instant.getUTCSeconds() !== 0 || instant.getUTCMilliseconds() !== 0) return null
        const fields = Object.fromEntries(new Intl.DateTimeFormat('en-GB', { timeZone: 'Asia/Shanghai', calendar: 'gregory', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(instant).filter((part)=>part.type!=='literal').map((part)=>[part.type,part.value]))
        const date=fields.year+'-'+fields.month+'-'+fields.day,time=fields.hour+':'+fields.minute
        return {date,time,dateTimeLocal:date+'T'+time}
      })()
      if (!metaExpectedSchedule || new Date(target.publishAt).getTime() <= Date.now()) throw new Error("meta_submit_schedule_readback_mismatch")
      const metaSubmitScheduleReadback = await js("(() => {const expected=" + JSON.stringify(metaExpectedSchedule) + ";const visible=(node)=>{const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.visibility!=='hidden'&&s.display!=='none'&&r.width>0&&r.height>0};const toggles=[...document.querySelectorAll(\"input[type='checkbox'],[role='switch'],[role='checkbox']\")].filter(visible).filter((node)=>{const label=node.getAttribute('aria-label')||node.closest('label')?.textContent||'';return ['Set date and time','设置日期和时间','设定日期和时间'].includes(String(label).trim())});const checked=(node)=>node?.checked===true||node?.getAttribute('aria-checked')==='true'||node?.getAttribute('data-checked')==='true';const dateTime=[...document.querySelectorAll(\"input[type='datetime-local']\")].filter(visible);const dates=[...document.querySelectorAll(\"input[type='date'],input[aria-label*='date' i],input[aria-label*='日期']\")].filter(visible);const times=[...document.querySelectorAll(\"input[type='time'],input[aria-label*='time' i],input[aria-label*='时间']\")].filter(visible);const exact=dateTime.length===1?dateTime[0].value===expected.dateTimeLocal:dates.length===1&&times.length===1&&dates[0].value===expected.date&&times[0].value===expected.time;return {toggleCount:toggles.length,checked:toggles.length===1&&checked(toggles[0]),exact}})()")
      if (!metaSubmitScheduleReadback?.checked || !metaSubmitScheduleReadback?.exact || metaSubmitScheduleReadback.toggleCount !== 1) throw new Error("meta_submit_schedule_readback_mismatch")
      submitStage = "final_control"
      selector = await js("(() => {const visible=(node)=>{const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.visibility!=='hidden'&&s.display!=='none'&&r.width>0&&r.height>0};const buttons=[...document.querySelectorAll(\"button,[role='button'],input[type='submit']\")].filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&['Schedule','定时发布'].includes(String(node.innerText||node.textContent||node.value||'').trim()));if(buttons.length!==1)return '';buttons[0].setAttribute('data-alpha-meta-submit-final','true');return '[data-alpha-meta-submit-final=\"true\"]'})()")
      if (!selector) throw new Error("meta_submit_schedule_final_button_ambiguous")
      verified = true
    } else if (target.platformCode === "csdn") {
      verified = await js("(() => {"+
        "const title="+JSON.stringify(target.title)+",body="+JSON.stringify(target.body)+",tags="+JSON.stringify(target.tags)+";"+
        "const titleNode=document.querySelector(\"input[placeholder*='标题'],textarea[placeholder*='标题'],#txtTitle\");"+
        "const bodyNode=document.querySelector(\"pre.editor__inner[contenteditable='true']\");"+
        "const actualBody=(bodyNode?.innerText||bodyNode?.textContent||'').replace(/\\r\\n?/g,'\\n');"+
        "const options=document.querySelector('.publish-article-modal__body');"+
        "const selectedTagTexts=options?[...options.querySelectorAll('*')].filter((node)=>{"+
          "const text=(node.textContent||'').trim();if(!tags.includes(text))return false;"+
          "for(let current=node,depth=0;current&&current!==options&&depth<5;current=current.parentElement,depth+=1){"+
            "const attributes=[...current.attributes].map((attribute)=>attribute.name+'='+attribute.value).join(' ').toLowerCase();"+
            "const className=String(current.className||'').toLowerCase();"+
            "const marker=attributes.includes('selected')||attributes.includes('chip')||attributes.includes('token')||/(^|[-_])(tag|chip|token)([-_]|$)/.test(className);"+
            "const removable=Boolean(current.querySelector(\"button,[role='button'],svg,[aria-label*='删除'],[aria-label*='移除']\"));"+
            "const explicit=current.getAttribute('aria-selected')==='true'||current.getAttribute('data-selected')==='true'||current.getAttribute('data-tag-selected')==='true';"+
            "if(marker&&(removable||explicit))return true;"+
          "}return false"+
        "}).map((node)=>(node.textContent||'').trim()):[];"+
        "const tagsMatch=tags.length>0&&tags.every((tag)=>selectedTagTexts.includes(tag));"+
        "const buttons=[...document.querySelectorAll('.publish-article-modal__body button.btn-b-red')].filter((node)=>{const s=getComputedStyle(node),r=node.getBoundingClientRect();return s.display!=='none'&&s.visibility!=='hidden'&&r.width>0&&r.height>0&&node.textContent.trim()==='发布文章'&&!node.disabled});"+
        "return titleNode?.value===title&&(actualBody===body||actualBody===body+'\\n')&&tagsMatch&&buttons.length===1})()")
      selector = '.publish-article-modal__body button.btn-b-red'
	    } else if (target.platformCode === "douyin") {
	      submitStage = "content"
	      const expectedBody = target.expectedBody ?? target.body ?? target.caption ?? ""
		const contentReadback = await js("(() => {"+
		  "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.display!=='none'&&style.visibility!=='hidden'&&rect.width>0&&rect.height>0};"+
		  "const titles=[...document.querySelectorAll(\"input[placeholder='添加作品标题'],input[placeholder*='标题']\")].filter(visible);"+
		  "const bodies=[...document.querySelectorAll(\"div[contenteditable='true'],textarea[placeholder*='简介'],textarea[placeholder*='描述']\")].filter(visible);"+
		  "const titleNode=titles.length===1?titles[0]:null,bodyNode=bodies.length===1?bodies[0]:null;"+
		  "const candidates=(node)=>[node?.value,node?.innerText,node?.textContent];if(bodyNode)bodyNode.setAttribute('data-alpha-douyin-submit-body','true');"+
		  "return {titleCandidates:candidates(titleNode),bodyCandidates:candidates(bodyNode),bodySelector:bodyNode?'[data-alpha-douyin-submit-body=\\\"true\\\"]':''};})()")
		submitStage = "title"
		if (!douyinCandidateMatches(contentReadback?.titleCandidates, target.title, normalizeDouyinTitle)) throw new Error("douyin_submit_title_readback_mismatch")
		submitStage = "body"
		const graphText = target.contentType === "graph_text"
		const bodyAndTopics = graphText ? await douyinBodyAndTopicReadback(contentReadback?.bodySelector) : contentReadback
		if (!douyinCandidateMatches(bodyAndTopics?.bodyCandidates, expectedBody, normalizeDouyinBody)) throw new Error("douyin_submit_body_readback_mismatch")
		if (graphText) {
		  submitStage = "topics"
		  const expectedTopics = douyinExpectedTopics(target.tags)
		  if (expectedTopics.length > 3 || bodyAndTopics?.invalid || !douyinExactTopicSetMatches(bodyAndTopics?.topics, expectedTopics)) {
		    throw new Error("douyin_submit_topic_readback_mismatch")
		  }
		}
	  if (target.publishAt) {
	    submitStage = "schedule"
	    const expectedSchedule = douyinExpectedSchedule(target.publishAt)
	    const scheduleSelector = await uniqueDouyinScheduleControl(["定时发布"], "schedule")
	    const immediateSelector = await uniqueDouyinScheduleControl(["立即发布"], "immediate")
	    const scheduleTimeSelector = await uniqueDouyinScheduleTimeInput()
	    const scheduleTimeReadback = scheduleTimeSelector ? await js("(() => document.querySelector('[data-alpha-douyin-submit-time=\\\"true\\\"]')?.value || '')()") : ''
	    if (!expectedSchedule || !scheduleSelector || !immediateSelector || !scheduleTimeSelector ||
	        !await douyinScheduleControlChecked(scheduleSelector) || await douyinScheduleControlChecked(immediateSelector) || scheduleTimeReadback !== expectedSchedule) {
	      throw new Error("douyin_submit_schedule_readback_mismatch")
	    }
	  }
	  submitStage = "final_control"
	  selector = await uniqueDouyinFinalButton()
	  if (!selector) throw new Error("douyin_submit_final_button_ambiguous")
	  verified = true
    } else if (target.platformCode === "youtube") {
      submitStage = "content"
      if (!target.publishAt) {
        submitStage = "immediate"
        throw new Error("youtube_immediate_submit_unsupported")
      }
      const titleSelector = await uniqueYouTubeSelector([
        "#title-textarea #textbox",
        "#title-textarea textarea",
        "#title-textarea input",
        "[contenteditable='true'][aria-label*='title' i]",
        "[contenteditable='true'][aria-label*='标题']",
      ], "submit-title")
      const descriptionSelector = await uniqueYouTubeSelector([
        "#description-textarea #textbox",
        "#description-textarea textarea",
        "#description-textarea [contenteditable='true']",
        "textarea[aria-label*='description' i]",
        "[contenteditable='true'][aria-label*='description' i]",
        "textarea[aria-label*='描述']",
        "[contenteditable='true'][aria-label*='描述']",
      ], "submit-description")
      if (!titleSelector) throw new Error("youtube_submit_title_readback_mismatch")
      if (!descriptionSelector) throw new Error("youtube_submit_description_readback_mismatch")
      const contentReadback = await js("(() => {const read=(selector)=>{const node=document.querySelector(selector);return node?.value??node?.innerText??node?.textContent??''};return {title:read(" + JSON.stringify(titleSelector) + "),description:read(" + JSON.stringify(descriptionSelector) + ")}})()")
      const expectedDescription = normalizeYouTubeDescription(target.body || target.caption)
      const actualDescription = normalizeYouTubeDescription(contentReadback?.description)
      if (contentReadback?.title !== target.title) throw new Error("youtube_submit_title_readback_mismatch")
      if (actualDescription !== expectedDescription && actualDescription !== expectedDescription + "\n") throw new Error("youtube_submit_description_readback_mismatch")

      submitStage = "schedule"
      const expectedSchedule = youtubeExpectedSchedule(target.publishAt)
      if (!expectedSchedule || new Date(target.publishAt).getTime() <= Date.now()) throw new Error("youtube_schedule_time_invalid")
      const scheduleSelector = await uniqueYouTubeScheduleOption("submit-schedule-mode")
      if (!scheduleSelector || !await youtubeScheduleSelected()) throw new Error("youtube_submit_schedule_mode_readback_mismatch")
      const dateSelector = await uniqueYouTubeSelector([
        "ytcp-datetime-picker ytcp-text-dropdown-trigger",
      ], "submit-schedule-date")
      const timeSelector = await uniqueYouTubeSelector([
        "input#time-of-day",
        "#time-of-day input",
        "ytcp-datetime-picker form input",
        "ytcp-datetime-picker tp-yt-paper-input input",
      ], "submit-schedule-time")
      if (!dateSelector) throw new Error("youtube_submit_schedule_date_control_missing")
      if (!timeSelector) throw new Error("youtube_submit_schedule_time_control_missing")
      if (!await youtubeScheduleReadbackStable(dateSelector, timeSelector, expectedSchedule)) throw new Error("youtube_submit_schedule_readback_mismatch")

      submitStage = "final_control"
      selector = await uniqueYouTubeScheduleFinalButton()
      if (!selector) throw new Error("youtube_submit_schedule_final_button_ambiguous")
      verified = true
    } else if (target.platformCode === "tiktok") {
      submitStage = "immediate"
      if (!target.publishAt) throw new Error("tiktok_immediate_submit_unsupported")
      submitStage = "content"
      const captionSelector = await uniqueTikTokSubmitSelector([
        "div.public-DraftEditor-content[contenteditable='true']",
        "div[contenteditable='true'][data-placeholder]",
        "div[contenteditable='true'][aria-label*='caption' i]",
        "textarea[placeholder*='caption' i]",
        "textarea[placeholder*='描述']",
      ], "caption")
      if (captionSelector === "__ambiguous__" || !captionSelector) throw new Error("tiktok_submit_caption_missing_or_ambiguous")
      const contentReadback = await js("(() => {const node=document.querySelector(" + JSON.stringify(captionSelector) + ");return (node?.value||node?.innerText||node?.textContent||'').replace(/\\r\\n?/g,'\\n')})()")
      const expectedCaption = String(target.body || target.caption || '').replace(/\\r\\n?/g,'\\n')
      if (contentReadback !== expectedCaption && contentReadback !== expectedCaption + "\\n") throw new Error("tiktok_submit_caption_readback_mismatch")
      if (target.title) {
        const titleSelector = await uniqueTikTokSubmitSelector(["input[placeholder*='title' i]", "input[placeholder*='标题']"], "title")
        if (titleSelector === "__ambiguous__" || !titleSelector) throw new Error("tiktok_submit_title_missing_or_ambiguous")
        const titleReadback = await js("(() => document.querySelector(" + JSON.stringify(titleSelector) + ")?.value || '')()")
        if (titleReadback !== target.title) throw new Error("tiktok_submit_title_readback_mismatch")
      }
      submitStage = "schedule"
      const expectedSchedule = tiktokExpectedSchedule(target.publishAt)
      if (!expectedSchedule || new Date(target.publishAt).getTime() <= Date.now()) throw new Error("tiktok_schedule_time_invalid")
      const scheduleSelector = await uniqueTikTokSubmitScheduleControl()
      if (scheduleSelector === "__ambiguous__" || !scheduleSelector || await readTikTokSubmitBoolean(scheduleSelector) !== true) throw new Error("tiktok_submit_schedule_mode_readback_mismatch")
      const scheduleInputs = await tiktokSubmitScheduleInputs()
      if (!scheduleInputs || scheduleInputs === "__ambiguous__" || !scheduleInputs.dateSelector || !scheduleInputs.timeSelector) throw new Error("tiktok_submit_schedule_controls_missing_or_ambiguous")
      if (!await tiktokSubmitScheduleReadbackStable(scheduleInputs, expectedSchedule)) throw new Error("tiktok_submit_schedule_readback_mismatch")
      submitStage = "final_control"
      selector = await uniqueTikTokSubmitFinalButton()
      if (selector === "__ambiguous__" || !selector) throw new Error("tiktok_submit_schedule_final_button_ambiguous")
      verified = true
    } else if (target.platformCode === "kuaishou") {
      submitStage = "immediate"
      if (!target.publishAt) throw new Error("kuaishou_immediate_submit_unsupported")
      const description = target.body || target.caption || ""
      const expectedBody = target.tags?.length
        ? description.replace(/\s+$/u, '') + "\n\n" + target.tags.map((tag) => "#" + tag).join(' ')
        : description
      submitStage = "content"
      if (target.title) {
        const titleSelector = await uniqueKuaishouSubmitSelector(["input[placeholder*='标题']", "textarea[placeholder*='标题']"], "title")
        if (titleSelector === "__ambiguous__" || !titleSelector) throw new Error("kuaishou_submit_title_readback_mismatch")
        const titleReadback = await js("(() => document.querySelector(" + JSON.stringify(titleSelector) + ")?.value || '')()")
        if (titleReadback !== target.title) throw new Error("kuaishou_submit_title_readback_mismatch")
      }
      const bodySelector = await uniqueKuaishouSubmitSelector(["textarea[placeholder*='描述']", "div[contenteditable='true']"], "body")
      if (bodySelector === "__ambiguous__" || !bodySelector) throw new Error("kuaishou_submit_body_readback_mismatch")
      const bodyReadback = await js("(() => {const node=document.querySelector(" + JSON.stringify(bodySelector) + ");return (node?.value||node?.innerText||node?.textContent||'').replace(/\\r\\n?/g,'\\n')})()")
      if (bodyReadback !== expectedBody && bodyReadback !== expectedBody + "\n") throw new Error("kuaishou_submit_body_readback_mismatch")
      submitStage = "schedule"
      const expectedSchedule = kuaishouExpectedSchedule(target.publishAt)
      if (!expectedSchedule || new Date(target.publishAt).getTime() <= Date.now()) throw new Error("kuaishou_schedule_time_invalid")
      const scheduleSelector = await uniqueKuaishouSubmitScheduleControl(["定时发布"], "schedule-mode")
      const immediateSelector = await uniqueKuaishouSubmitScheduleControl(["立即发布"], "immediate-mode")
      if (scheduleSelector === "__ambiguous__" || immediateSelector === "__ambiguous__" || !scheduleSelector || !immediateSelector ||
          await readKuaishouSubmitBoolean(scheduleSelector) !== true || await readKuaishouSubmitBoolean(immediateSelector) !== false) {
        throw new Error("kuaishou_submit_schedule_readback_mismatch")
      }
      const scheduleInputSelector = await uniqueKuaishouSubmitScheduleInput()
      if (scheduleInputSelector === "__ambiguous__" || !scheduleInputSelector || !await kuaishouScheduleReadbackStable(scheduleInputSelector, expectedSchedule)) {
        throw new Error("kuaishou_submit_schedule_readback_mismatch")
      }
      submitStage = "final_control"
      selector = await uniqueKuaishouSubmitFinalButton()
      if (selector === "__ambiguous__" || !selector) throw new Error("kuaishou_submit_final_button_ambiguous")
      verified = true
    } else if (target.platformCode === "wechat_video") {
      submitStage = "immediate"
      if (!target.publishAt) throw new Error("wechat_video_immediate_submit_unsupported")
      submitStage = "content"
      const titleSelector = await uniqueWechatVideoSubmitField(["input[placeholder*='标题']"], "title")
      const bodySelector = await uniqueWechatVideoSubmitField([
        ".input-editor[data-placeholder='添加描述']",
        "div[contenteditable='true'][data-placeholder='添加描述']",
        "textarea[placeholder*='描述']",
      ], "body")
      if (titleSelector === "__ambiguous__" || !titleSelector || bodySelector === "__ambiguous__" || !bodySelector) {
        throw new Error("wechat_video_submit_content_missing_or_ambiguous")
      }
      const titleReadback = await wechatVideoSubmitReadbackField(titleSelector)
      if (titleReadback !== target.title) throw new Error("wechat_video_submit_title_readback_mismatch")
      if (!await wechatVideoSubmitBodyMatches(bodySelector, target.body || target.caption || "", target.tags)) {
        throw new Error("wechat_video_submit_body_readback_mismatch")
      }
      submitStage = "schedule"
      const expectedSchedule = wechatVideoExpectedSchedule(target.publishAt)
      if (!expectedSchedule || new Date(target.publishAt).getTime() <= Date.now()) throw new Error("wechat_video_schedule_time_invalid")
      const scheduleSelector = await uniqueWechatVideoSubmitScheduleControl()
      const immediateSelector = await uniqueWechatVideoScheduleControl(["立即发表", "立即发布"], "submit-immediate-mode")
      if (scheduleSelector === "__ambiguous__" || immediateSelector === "__ambiguous__" || !scheduleSelector || !immediateSelector ||
          await readWechatVideoSubmitBoolean(scheduleSelector) !== true || await readWechatVideoSubmitBoolean(immediateSelector) === true) {
        throw new Error("wechat_video_submit_schedule_readback_mismatch")
      }
      const scheduleInputSelector = await uniqueWechatVideoSubmitScheduleInput()
      if (scheduleInputSelector === "__ambiguous__" || !scheduleInputSelector ||
          !await wechatVideoSubmitScheduleReadbackStable(scheduleInputSelector, expectedSchedule, scheduleSelector)) {
        throw new Error("wechat_video_submit_schedule_readback_mismatch")
      }
      submitStage = "final_control"
      selector = await uniqueWechatVideoSubmitFinalButton()
      if (selector === "__ambiguous__" || !selector) throw new Error("wechat_video_submit_final_button_ambiguous")
      verified = true
    } else if (target.platformCode === "wechat_mp") {
      submitStage = "composer"
      const currentEditor = await js("(() => {" +
        "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0};" +
        "const text=(document.body?.innerText||'').slice(0,12000),url=String(location.href||''),expectedTitle=" + JSON.stringify(target.title) + ",expectedBody=" + JSON.stringify(target.body) + ",expectedAuthor=" + JSON.stringify(target.author) + ",expectedPublishType=" + JSON.stringify(target.publishType) + ";" +
        "const unique=(selectors)=>[...new Set(selectors.flatMap((selector)=>[...document.querySelectorAll(selector)]))].filter(visible),read=(node)=>String(node?.value??node?.innerText??node?.textContent??'').replace(/\\r\\n?/g,'\\n'),checked=(node)=>{const controls=[node,...node.querySelectorAll(\"input[type='checkbox'],[role='checkbox'],[role='switch'],[aria-checked]\")];for(const control of controls){if(control?.checked===true||control?.checked===false)return control.checked;for(const name of ['aria-checked','aria-selected','data-selected']){const value=control?.getAttribute(name);if(value==='true'||value==='false')return value==='true'}if(control?.hasAttribute('checked')||control?.hasAttribute('selected')||control?.classList?.contains('selected')||control?.classList?.contains('active')||control?.classList?.contains('checked'))return true}return null};" +
        "const titles=unique(['.title-editor__input .ProseMirror[contenteditable=\\\"true\\\"]','input[placeholder*=\\\"标题\\\"]','textarea[placeholder*=\\\"标题\\\"]']),bodies=unique(['.mock-iframe-body .ProseMirror[contenteditable=\\\"true\\\"]']),covers=unique(['.js_cover_preview_new']),authors=unique(['input[placeholder*=\\\"作者\\\"]','input[name*=\\\"author\\\" i]','textarea[placeholder*=\\\"作者\\\"]']),comments=unique(['input.js_interaction_setting',\"[role='checkbox'][aria-label*='留言']\",\"[role='switch'][aria-label*='留言']\"]),originals=unique(['.js_edit_ori input.js_ori_setting_checkbox',\"[role='checkbox'][aria-label*='原创']\",\"[role='switch'][aria-label*='原创']\"]),originalStates=unique(['.js_edit_ori .js_unset_original_title']),collections=unique(['.js_article_tags_content']),sources=unique(['.js_claim_source_selected']);" +
        "const bodyValue=bodies.length===1?read(bodies[0]):'',coverReady=covers.length===1&&String(getComputedStyle(covers[0]).backgroundImage||'')!=='none',originalOK=originals.length===1?checked(originals[0])===false:originals.length===0&&originalStates.length===1&&read(originalStates[0]).trim()==='未声明',settingsOK=expectedPublishType==='publish'&&titles.length===1&&read(titles[0])===expectedTitle&&bodies.length===1&&(bodyValue===expectedBody||bodyValue===expectedBody+'\\n')&&coverReady&&authors.length===1&&read(authors[0])===expectedAuthor&&comments.length===1&&checked(comments[0])===false&&originalOK&&collections.length===1&&read(collections[0]).trim()==='未添加'&&sources.length===1&&read(sources[0]).trim()==='无需声明';" +
        "return {url,isAppmsg:(()=>{try{const parsed=new URL(url);return parsed.protocol==='https:'&&parsed.hostname==='mp.weixin.qq.com'&&parsed.pathname==='/cgi-bin/appmsg'}catch{return false}})(),titleCount:titles.length,title:titles.length===1?read(titles[0]):'',settingsOK,login:/登录|请先登录|sign in|log in/i.test(text)||/login|signin|passport/i.test(url),verification:/扫码|二维码|微信验证|验证码|安全验证|captcha|challenge/i.test(text)||/captcha|verify|challenge/i.test(url)};})()")
      if (currentEditor?.login || currentEditor?.verification) throw new Error("platform_verification_required")
      if (!currentEditor?.isAppmsg) throw new Error("wechat_mp_submit_editor_missing_or_ambiguous")
      const current = await currentTab()
      const currentID = String(current?.targetId ?? current?.id ?? "")
      const editorTabs = (await listTabs()).filter((tab) => {
        try {
          const parsed = new URL(String(tab?.url || ""))
          return parsed.protocol === "https:" && parsed.hostname === "mp.weixin.qq.com" && parsed.pathname === "/cgi-bin/appmsg"
        } catch { return false }
      })
      const editorID = editorTabs.length === 1 ? String(editorTabs[0]?.targetId ?? editorTabs[0]?.id ?? "") : ""
      if (!editorID || currentID !== editorID) throw new Error("wechat_mp_submit_editor_missing_or_ambiguous")
      submitStage = "content"
      if (currentEditor.titleCount !== 1 || currentEditor.title !== target.title) throw new Error("wechat_mp_submit_title_readback_mismatch")
      submitStage = "settings"
      if (!currentEditor.settingsOK) throw new Error("wechat_mp_submit_settings_readback_mismatch")
      const finalControl = await js("(() => {" +
        "const visible=(node)=>{if(!node)return false;const style=getComputedStyle(node),rect=node.getBoundingClientRect();return style.visibility!=='hidden'&&style.display!=='none'&&rect.width>0&&rect.height>0},readBoolean=(node)=>{const controls=[node,...node.querySelectorAll(\"input[type='checkbox'],[role='checkbox'],[role='switch'],[aria-checked]\")];for(const control of controls){if(control?.checked===true||control?.checked===false)return control.checked;for(const name of ['aria-checked','aria-selected','data-selected']){const value=control?.getAttribute(name);if(value==='true'||value==='false')return value==='true'}if(control?.hasAttribute('checked')||control?.hasAttribute('selected')||control?.classList?.contains('selected')||control?.classList?.contains('active')||control?.classList?.contains('checked'))return true}return null};" +
        "let parsed;try{parsed=new URL(location.href)}catch{return {selector:'',settingsOK:false}}if(parsed.protocol!=='https:'||parsed.hostname!=='mp.weixin.qq.com'||parsed.pathname!=='/cgi-bin/appmsg')return {selector:'',settingsOK:false};const dialogs=[...document.querySelectorAll('.new_mass_send_dialog .weui-desktop-dialog')].filter(visible);if(dialogs.length!==1)return {selector:dialogs.length>1?'__ambiguous__':'',settingsOK:false};const dialog=dialogs[0],controlFor=(label,optional)=>{const labels=[...dialog.querySelectorAll('label,span,div')].filter((node)=>visible(node)&&(node.innerText||node.textContent||'').trim()===label),rows=[...new Set(labels.map((node)=>node.closest('.mass-send__td')||node.parentElement).filter(Boolean))],controls=[...new Set(rows.flatMap((row)=>[...row.querySelectorAll(\"input[type='checkbox'],[role='checkbox'],[role='switch']\")]).filter(visible))];if(controls.length===0&&optional)return '__absent__';return controls.length===1?controls[0]:null};const notify=controlFor('群发通知',false),groupNotify=controlFor('分组通知',true),schedule=controlFor('定时发表',false),settingsOK=readBoolean(notify)===false&&(groupNotify==='__absent__'||readBoolean(groupNotify)===false)&&readBoolean(schedule)===false;const buttons=[...dialog.querySelectorAll('button.weui-desktop-btn_primary')].filter((node)=>visible(node)&&!node.disabled&&node.getAttribute('aria-disabled')!=='true'&&(node.innerText||node.textContent||'').trim()==='发表');if(buttons.length!==1)return {selector:buttons.length>1?'__ambiguous__':'',settingsOK};for(const node of document.querySelectorAll('[data-alpha-wechat-mp-submit-final]'))node.removeAttribute('data-alpha-wechat-mp-submit-final');buttons[0].setAttribute('data-alpha-wechat-mp-submit-final','true');return {selector:'[data-alpha-wechat-mp-submit-final=\\\"true\\\"]',settingsOK};})()")
      if (!finalControl?.settingsOK) throw new Error("wechat_mp_submit_settings_readback_mismatch")
      submitStage = "final_control"
      selector = finalControl?.selector || ""
      if (selector === "__ambiguous__" || !selector) throw new Error("wechat_mp_submit_final_button_ambiguous")
      verified = true
    }
    if (!verified || !selector) throw new Error("frozen_form_mismatch")
    await alphaInteractionPause(target.finalPauseMs, 900, 1500)
    try {
	  if (target.platformCode === "wechat_video") {
	    clicked = await clickWechatVideoFrameControl("submit-final")
	    if (!clicked) throw new Error("wechat_video_submit_final_button_missing")
	  } else {
	    await click(selector, { label: 'submit approved publish' })
	    clicked = true
	  }
    } catch {
      status = "result_unknown"; errorCode = "EGO_SUBMIT_RESULT_UNKNOWN"; clicked = true
    }
    if (clicked && status !== "result_unknown") {
      await new Promise((resolve) => setTimeout(resolve, 2500))
	  const evidence = await js("(() => ({url:location.href,text:(document.body?.innerText||'').slice(0,5000)}))()")
	  if (await alphaPlatformChallengeVisible()) {
	    status = "result_unknown"
	    errorCode = "LOGIN_REQUIRED"
	  } else if (target.platformCode === "youtube") {
	    status = "submitted_pending_verification"
	    errorCode = "EGO_BACKEND_VERIFICATION_REQUIRED"
	  } else if (target.platformCode === "kuaishou") {
	    const success = /发布成功/.test(evidence.text)
	    status = success ? "submitted_pending_verification" : "result_unknown"
	    errorCode = success ? "EGO_BACKEND_VERIFICATION_REQUIRED" : "EGO_SUBMIT_RESULT_UNKNOWN"
	  } else if (target.platformCode === "wechat_video") {
	    const frameEvidence = await js("(() => {const prefix=" + JSON.stringify(wechatVideoFramePrefix) + ";const frames=[...document.querySelectorAll('iframe')].filter((node)=>String(node.getAttribute('src')||node.src||'').startsWith(prefix));if(frames.length!==1||!frames[0].contentDocument)return {text:'',confirmation:false};const text=(frames[0].contentDocument.body?.innerText||'').slice(0,5000);const confirmation=/确认发表|声明原创|内容声明/.test(text);return {text,confirmation}})()")
	    const success = /定时发表成功|发表成功|发布成功/.test(frameEvidence?.text || '') && !frameEvidence?.confirmation
	    status = success ? "submitted_pending_verification" : "result_unknown"
	    errorCode = success ? "EGO_BACKEND_VERIFICATION_REQUIRED" : "EGO_SUBMIT_RESULT_UNKNOWN"
	  } else if (target.platformCode === "wechat_mp") {
	    const success = /发表成功|发布成功|群发成功/.test(evidence.text)
	    status = success ? "submitted_pending_verification" : "result_unknown"
	    errorCode = success ? "EGO_BACKEND_VERIFICATION_REQUIRED" : "EGO_SUBMIT_RESULT_UNKNOWN"
	  } else {
	    const success = target.platformCode === 'csdn'
		  ? /发布成功|审核中/.test(evidence.text)
		  : /发布成功/.test(evidence.text)
	    status = success ? "submitted_pending_verification" : "result_unknown"
	    errorCode = success ? "EGO_BACKEND_VERIFICATION_REQUIRED" : "EGO_SUBMIT_RESULT_UNKNOWN"
	  }
    }
  } catch (error) {
    if (/platform_verification_required/i.test(String(error?.message || error))) {
      status = clicked ? "result_unknown" : "failed"
      errorCode = "LOGIN_REQUIRED"
    } else if (target.platformCode === "douyin" && !clicked) {
      const stageCodes = {
        composer: "EGO_DOUYIN_SUBMIT_COMPOSER_MISSING",
        content: "EGO_DOUYIN_SUBMIT_CONTENT_READBACK_MISMATCH",
        title: "EGO_DOUYIN_SUBMIT_TITLE_READBACK_MISMATCH",
        body: "EGO_DOUYIN_SUBMIT_BODY_READBACK_MISMATCH",
        topics: "EGO_DOUYIN_SUBMIT_TOPIC_READBACK_MISMATCH",
        schedule: "EGO_DOUYIN_SUBMIT_SCHEDULE_READBACK_MISMATCH",
        final_control: "EGO_DOUYIN_SUBMIT_FINAL_CONTROL_MISSING"
      }
      errorCode = stageCodes[submitStage] || errorCode
    } else if (target.platformCode === "youtube" && !clicked) {
      const stageCodes = {
        composer: "EGO_YOUTUBE_SUBMIT_COMPOSER_MISSING",
        immediate: "EGO_YOUTUBE_IMMEDIATE_SUBMIT_UNSUPPORTED",
        content: "EGO_YOUTUBE_SUBMIT_CONTENT_READBACK_MISMATCH",
        schedule: "EGO_YOUTUBE_SUBMIT_SCHEDULE_READBACK_MISMATCH",
        final_control: "EGO_YOUTUBE_SUBMIT_FINAL_CONTROL_MISSING"
      }
      errorCode = stageCodes[submitStage] || errorCode
    } else if (target.platformCode === "tiktok" && !clicked) {
      const stageCodes = {
        composer: "EGO_TIKTOK_SUBMIT_COMPOSER_MISSING",
        immediate: "EGO_TIKTOK_IMMEDIATE_SUBMIT_UNSUPPORTED",
        content: "EGO_TIKTOK_SUBMIT_CAPTION_READBACK_MISMATCH",
        schedule: "EGO_TIKTOK_SUBMIT_SCHEDULE_READBACK_MISMATCH",
        final_control: "EGO_TIKTOK_SUBMIT_FINAL_CONTROL_MISSING"
      }
      errorCode = stageCodes[submitStage] || errorCode
    } else if (target.platformCode === "kuaishou" && !clicked) {
      const stageCodes = {
        composer: "EGO_KUAISHOU_SUBMIT_COMPOSER_MISSING",
        immediate: "EGO_KUAISHOU_IMMEDIATE_SUBMIT_UNSUPPORTED",
        content: "EGO_KUAISHOU_SUBMIT_CONTENT_READBACK_MISMATCH",
        schedule: "EGO_KUAISHOU_SUBMIT_SCHEDULE_READBACK_MISMATCH",
        final_control: "EGO_KUAISHOU_SUBMIT_FINAL_CONTROL_MISSING"
      }
      errorCode = stageCodes[submitStage] || errorCode
	} else if (target.platformCode === "wechat_video" && !clicked) {
	  const stageCodes = {
	    composer: "EGO_WECHAT_VIDEO_SUBMIT_COMPOSER_MISSING",
	    immediate: "EGO_WECHAT_VIDEO_IMMEDIATE_SUBMIT_UNSUPPORTED",
	    content: "EGO_WECHAT_VIDEO_SUBMIT_CONTENT_READBACK_MISMATCH",
	    schedule: "EGO_WECHAT_VIDEO_SUBMIT_SCHEDULE_READBACK_MISMATCH",
	    final_control: "EGO_WECHAT_VIDEO_SUBMIT_FINAL_CONTROL_MISSING"
	  }
	  errorCode = stageCodes[submitStage] || errorCode
	} else if (target.platformCode === "wechat_mp" && !clicked) {
	  const stageCodes = {
	    composer: "EGO_WECHAT_MP_SUBMIT_COMPOSER_MISSING",
	    content: "EGO_WECHAT_MP_SUBMIT_TITLE_READBACK_MISMATCH",
	    settings: "EGO_WECHAT_MP_SUBMIT_SETTINGS_READBACK_MISMATCH",
	    final_control: "EGO_WECHAT_MP_SUBMIT_FINAL_CONTROL_MISSING"
	  }
	  errorCode = stageCodes[submitStage] || errorCode
    }
    // No trusted click occurred. The target failed without creating a publish
    // task that requires user takeover; login challenges are reported above.
  }
  submitResults.push({ platformCode: target.platformCode, status, errorCode, clicked })
}
cliLog(%q + JSON.stringify({
  done: true,
  results: submitResults,
}))
`, spaceID, jsErrorLiteral(ErrSpaceBindingMissing), jsErrorLiteral(ErrSpaceUserControlled), spaceID, targetsJSON, resultPrefix)
}
