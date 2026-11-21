'use strict';

const fs = require('node:fs');
const crypto = require('node:crypto');
const path = require('node:path');
const { createAgentControl } = require('./agent_overlay.cjs');

let clickEvidenceBindingSequence = 0;
let fileSelectionBindingSequence = 0;

const COMMON_PLATFORM_VERIFICATION_TEXTS = Object.freeze([
  '请完成验证后继续',
  '请先完成安全验证',
  '安全验证',
  '身份验证',
  '验证身份',
  '扫码验证',
  'Verify it’s you',
  "Verify it's you",
  'Security check',
  'Complete the verification',
  'Confirm your identity',
]);

const COMMON_PLATFORM_VERIFICATION_SELECTORS = Object.freeze([
  "iframe[src*='recaptcha' i]",
  "iframe[src*='hcaptcha' i]",
  "[data-testid*='captcha' i]",
  "[class*='captcha' i]",
  "[id*='captcha' i]",
]);

const PLATFORM_RULES = Object.freeze({
  xhs: {
    composerUrl: 'https://creator.xiaohongshu.com/publish/publish',
    verifiedAbsentDefaults: true,
    verifiedAbsentComposer: 'xhs',
    // A prepared XHS draft removes its initial upload control. Reusing that
    // tab would silently mix a prior task's media into the next task.
    alwaysFreshComposer: true,
    composerURLs: {
      video: 'https://creator.xiaohongshu.com/publish/publish?from=menu&target=video',
      graph_text: 'https://creator.xiaohongshu.com/publish/publish?from=menu&target=image',
    },
    mediaUploadTriggerNames: {
      image: ['上传图片', '添加图片'],
    },
    title: ["input[placeholder*='标题']", "textarea[placeholder*='标题']"],
    body: ["textarea[placeholder*='正文']", "div[contenteditable='true']"],
    bodyWhitespaceEquivalent: true,
    topicControl: {
      mode: 'trigger_picker',
      triggerSelectors: ['#topicBtn'],
      strictTriggerSelectors: true,
      triggerNames: ['#话题', '#添加话题', '添加话题', '话题'],
      commitKey: 'Space',
      preserveNativeFocus: true,
      incrementalExactSuggestions: true,
      exactNativeTopicSet: true,
      bodyWhitespaceEquivalent: true,
      nativeTopicSelectors: ['a.tiptap-topic[data-topic]'],
      removeUnexpectedNativeTopics: true,
      maxNativeTopicRemovals: 10,
      // The official dropdown can expire quickly once the exact result
      // arrives; per-character readiness above is the wait, then click it.
      settleMs: 100,
      characterSettleTimeoutMs: 5000,
      betweenTopicsSettleMs: 750,
      requireTriggerMarker: true,
      preferCJK: true,
      maxTopics: 3,
    },
    postUploadSettleMs: 750,
    uploadRootSelector: '.publish-page',
    // The image composer mounts the reviewed editor root only after the
    // platform accepts the selected files. The file-list readback remains
    // mandatory; after selection we require the unique root, one new preview
    // per frozen item, cleared progress, and no upload error.
    uploadRootMayMountAfterSelection: true,
    requireFileSelectionReadback: true,
    requirePreviewIncrementPerItem: true,
    requireUploadCompletionAtPrepare: true,
    uploadCompletionTimeoutMs: 90000,
    // The video composer can render its preview inside a closed component.
    // When no custom cover is frozen, this native first-frame marker appears
    // only after the platform has accepted the selected video.
    uploadReadyNames: ['默认截取第一帧作为封面，优质的封面会吸引更多人浏览笔记'],
    submitReadyTimeoutMs: 90000,
    submitRootSelector: '.publish-page',
    strictSubmitSelectors: true,
    closedShadowSubmit: true,
    coverControl: {
      // A frozen cover is a distinct asset. The native default-first-frame
      // notice is not evidence that the frozen cover was selected.
      requireExplicitUpload: true,
      previewSelectors: ['.cover-plugin-preview .cover .default'],
      triggerSelectors: ['.cover-plugin-preview .operator.default'],
      dialogSelectors: ['.cover-modal'],
      uploadSelectors: ["input[type='file'][accept*='image' i]"],
      confirmNames: ['确定'],
      requirePreviewBackgroundChange: true,
      nativeDefaultNames: ['默认截取第一帧作为封面，优质的封面会吸引更多人浏览笔记'],
    },
    submitSelectors: ['xhs-publish-btn'],
    submitNames: ['发布', '立即发布', '定时发布'],
    successText: ['发布成功', '已发布'],
    // These queries are the reviewed XHS setting operations from the shared
    // selector catalog. Other platforms remain explicitly unverified until
    // equivalent evidence is added; the worker must not guess their DOM.
    settingControls: {
      visibility: {
        type: 'enum', noOpValues: ['public'],
        options: {
          public: {
            selectors: ["[data-testid='visibility-public']", "[role='radio'][aria-label*='公开']"],
            names: ['公开'],
            selectedTextSelectors: ['.permission-card-select .d-select-content'],
            selectedTextValues: ['公开可见'],
          },
          friends: { names: ['互关好友'] },
          private: { selectors: ["[data-testid='visibility-private']", "[role='radio'][aria-label*='私密']", "[role='radio'][aria-label*='仅自己']"], names: ['仅自己', '私密'] },
        },
      },
      comment_enabled: { type: 'boolean', verifiedAbsentValue: true, selectors: ["[data-testid='comment-enabled']", "[role='switch'][aria-label*='评论']"], names: ['允许评论'] },
      declare_original: { type: 'boolean', verifiedAbsentValue: false, selectors: ["[data-testid='declare-original']", "[role='checkbox'][aria-label*='原创']"], names: ['声明原创'] },
      allow_duet: {
        type: 'boolean',
        verifiedAbsentValue: false,
        scopedLabel: '允许合拍',
        wrapperSelector: '.custom-switch-wrapper',
        labelSelector: '.custom-switch-text-content',
        stateSelector: ".custom-switch-switch input[type='checkbox']",
        clickSelector: '.custom-switch-switch .d-switch-simulator',
      },
      allow_copy: {
        type: 'boolean',
        verifiedAbsentValue: false,
        scopedLabel: '允许正文复制',
        wrapperSelector: '.custom-switch-wrapper',
        labelSelector: '.custom-switch-text-content',
        stateSelector: ".custom-switch-switch input[type='checkbox']",
        clickSelector: '.custom-switch-switch .d-switch-simulator',
      },
      location: {
        type: 'text',
        emptyReadback: {
          selectors: ['.address-card-select .d-select-placeholder'],
          values: ['添加地点'],
        },
        selectors: [
          '.address-card-select .d-select-input-filter:not(.hide) input.d-text',
          "[class*='location' i] input",
          "[class*='place' i] input",
          "input[placeholder*='地点']",
          "input[placeholder*='位置']",
          "[placeholder*='搜索地点']",
          "[data-placeholder*='搜索地点']",
        ],
        triggerSelectors: ['.address-card-select .d-select-suffix', '.address-card-select .d-select-main'],
        triggerNames: ['添加地点', '位置'],
        optionSelectors: [
          '.d-popover.custom-dropdown-44 .d-grid-item',
          "[role='listbox'] [role='option']",
          "[role='option']",
          "[class*='location' i] [class*='item' i]",
          "[class*='place' i] [class*='item' i]",
        ],
        selectedTextSelectors: ['.d-select-content'],
        selectedTextAncestorXPath: "ancestor::*[contains(concat(' ', normalize-space(@class), ' '), ' address-card-select ')][1]",
        selectedTextMustMatchValue: true,
        fillDirectly: true,
        chooseFuzzy: true,
        chooseFirstResult: true,
        minimumFuzzyTokenMatches: 2,
      },
      collection: {
        type: 'text',
        selectors: ["input[placeholder*='合集']"],
        triggerNames: ['添加合集', '合集'],
        chooseExact: true,
        emptyReadback: {
          selectors: ['.collection-plugin-wrapper .collection-plugin-content-title'],
          values: ['加入合集'],
        },
      },
      group: {
        type: 'text',
        selectors: ["input[placeholder*='群聊']"],
        triggerNames: ['关联群聊'],
        chooseExact: true,
        emptyReadback: {
          selectors: ['.group-card-select .d-select-placeholder'],
          values: ['选择群聊'],
        },
      },
      related_media: {
        type: 'text',
        selectors: ["input[placeholder*='媒体']"],
        triggerNames: ['关联媒体'],
        chooseExact: true,
        emptyReadback: {
          selectors: ['.setting-card-wrapper .setting-card-text'],
          values: ['引用笔记'],
        },
      },
      content_source: {
        type: 'enum', triggerNames: ['创作来源', '内容来源声明', '添加内容类型声明'], noOpValues: ['none'],
        emptyReadback: {
          selectors: ['.publish-page-content-setting-content .custom-select-44 .d-select-content'],
          values: ['添加内容类型声明'],
        },
        options: {
          fiction: { names: ['虚构演绎仅供娱乐'] },
          ai: {
            names: ['笔记含AI合成内容', '笔记含 AI 合成内容'],
            choiceSelectors: ['.declaration-drop-down .d-grid-item'],
            selectedTextSelectors: ['.publish-page-content-setting-content .custom-select-44 .d-select-content'],
            selectedTextValues: ['笔记含AI合成内容', '笔记含 AI 合成内容'],
          },
          self_disclosed: { names: ['已在正文中自主标注'] }, original: { names: ['自主拍摄'] }, reprint: { names: ['来源转载'] },
        },
      },
    },
    scheduleControl: {
      toggleNames: ['定时发布'],
      scopedLabel: '定时发布',
      nativeTyping: true,
      dateTimeFormat: 'yyyy-MM-dd HH:mm',
      wrapperSelector: '.custom-switch-wrapper',
      labelSelector: '.custom-switch-text-content',
      stateSelector: ".custom-switch-switch input[type='checkbox']",
      clickSelector: '.custom-switch-switch .d-switch-simulator',
      dateTimeInputs: ["input[type='datetime-local']", "input[placeholder='选择发布时间']", "input[aria-label='发布时间']"],
      dateInputs: ["input[type='date']", "input[placeholder='选择日期']"],
      timeInputs: ["input[type='time']", "input[placeholder='选择时间']"],
    },
  },
  douyin: {
    composerUrl: 'https://creator.douyin.com/creator-micro/content/upload',
    composerURLPrefixes: {
      video: ['https://creator.douyin.com/creator-micro/content/post/video'],
    },
    entryFlows: {
      // The console does not expose a stable graph-text URL in its navigation.
      // Enter through the verified home card and accept only the creator
      // content namespace reached by that exact action.
      graph_text: {
        entryURL: 'https://creator.douyin.com/creator-micro/home',
        entryNames: ['发布图文'],
        // Some background creator sessions render the card but do not route
        // its nested text click. This is the same official intermediate URL,
        // used only when the reviewed card did not reach the upload page.
        fallbackEntryURL: 'https://creator.douyin.com/creator-micro/content/upload?default-tab=3',
        intermediateURLPrefixes: ['https://creator.douyin.com/creator-micro/content/upload'],
        uploadOnIntermediate: true,
        requiredURLPrefixes: ['https://creator.douyin.com/creator-micro/content/post/image'],
        uploadRootSelector: '.ant-carousel',
        uploadRootMayMountAfterSelection: true,
        requireFileSelectionReadback: true,
        requirePreviewIncrementPerItem: true,
        requireUploadCompletionAtPrepare: true,
        uploadCompletionTimeoutMs: 90000,
        submitReadyTimeoutMs: 90000,
        immutableMediaPayload: true,
        uploadPreviewSourcePattern: '^https://p\\d+-creator-media-private\\.douyin\\.com/',
        uploadStatusPageWide: true,
        uploadCompletionStableMs: 1500,
      },
    },
    verifiedAbsentDefaults: true,
    verifiedAbsentComposer: 'douyin',
    title: ["input[placeholder='添加作品标题']", "input[placeholder*='标题']"],
    body: ["div[contenteditable='true']", "textarea[placeholder*='简介']", "textarea[placeholder*='描述']"],
    // Native topics can rewrite the rich-text line boundaries after the body is
    // filled. Revalidation still compares every non-whitespace character.
    bodyWhitespaceEquivalent: true,
    topicControl: {
      mode: 'trigger_picker',
      triggerNames: ['#添加话题', '添加话题'],
      commitKey: 'Space',
      preserveNativeFocus: true,
      settleMs: 1500,
      stripFrozenTrailingTags: true,
      nativeTopicSelectors: ["[data-mention='#']", "[data-topic-selected='true']"],
    },
    dismissNames: ['我知道了'],
    coverControl: {
      triggerNames: ['选择封面', '设置封面'],
      panelMarkerNames: ['AI生成封面', '设置竖封面'],
      uploadTriggerNames: ['上传封面'],
      uploadInputAncestorLevels: 2,
      uploadInputSelectors: ['input.semi-upload-hidden-input:not(.semi-upload-hidden-input-replace)'],
      useVerifiedFilePaths: true,
      previewCanvasSelectors: ['#vertical_coverCanvas'],
      // Chromium may expose a stable black GPU canvas even after Douyin has
      // accepted the image. The frozen hash, uniquely scoped input, enabled
      // confirm control, and closed editor are the reliable commit evidence.
      requirePreviewCanvasChange: false,
      uploadProcessingTimeoutMs: 90000,
      committedPreviewNames: ['竖封面3:4'],
      committedPreviewAncestorLevels: 3,
      confirmNames: ['完成', '确认'],
      postConfirmNames: ['暂不设置', '我知道了'],
    },
    strictSubmitNames: true,
    submitNames: ['发布'],
    successText: ['发布成功', '作品管理'],
    settingControls: {
      activity: { type: 'text', selectors: ["input[placeholder*='活动']"], triggerNames: ['平台活动', '参与活动'], chooseExact: true },
      collection: {
        type: 'text',
        selectors: ["input[placeholder*='合集']"],
        triggerNames: ['添加合集', '合集'],
        chooseExact: true,
        emptyReadback: {
          selectors: ['.semi-select-selection-text'],
          values: ['不选择合集', '请选择合集'],
        },
      },
      extension_type: {
        type: 'enum', noOpValues: ['none'],
        options: {
          location: { names: ['位置', '添加位置'] },
          film_tv: { names: ['影视演艺'] },
          mini_program: { names: ['小程序'] },
          mark_objects: { names: ['标记万物'] },
        },
      },
      location: {
        type: 'text',
        parentSetting: { key: 'extension_type', enabledValue: 'location' },
        selectors: [
          "#douyin_creator_pc_anchor_jump .semi-select-filterable",
          "input[placeholder*='地理位置']",
          "[data-placeholder*='地理位置']",
          "input[placeholder*='位置']",
          "input[placeholder*='地点']",
        ],
        triggerNames: ['添加位置', '位置'],
        optionSelectors: [
          "[role='listbox'] [role='option']",
          "[role='option']",
          "[class*='location' i] [class*='item' i]",
          "[class*='location' i] [class*='option' i]",
        ],
        chooseFuzzy: true,
        chooseFirstResult: true,
        minimumFuzzyTokenMatches: 2,
        keyboardFilterable: true,
      },
      hotspot: {
        type: 'text',
        selectors: ["input[placeholder*='热点']"],
        triggerNames: ['关联热点'],
        chooseExact: true,
        emptyReadback: {
          selectors: ['.semi-select-selection-text'],
          values: ['点击输入热点词'],
        },
      },
      music: {
        type: 'text',
        selectors: ["input[placeholder*='音乐']"],
        triggerNames: ['选择音乐', '音乐'],
        chooseExact: true,
        emptyReadback: {
          selectors: ["[class^='desc-']"],
          values: ['点击添加合适作品风格音乐'],
        },
      },
      allow_download: {
        type: 'enum',
        options: {
          true: { names: ['允许'] },
          false: { names: ['不允许'] },
        },
      },
      visibility: {
        type: 'enum', triggerNames: ['可见范围'], noOpValues: ['public'],
        options: {
          public: { names: ['公开'] }, friends: { names: ['好友可见', '好友'] }, private: { names: ['仅自己可见', '仅自己'] },
        },
      },
      comment_enabled: {
        type: 'boolean',
        verifiedAbsentValue: true,
        selectors: [
          "input[type='checkbox'][aria-label='允许评论']",
          "input[type='checkbox'][aria-label='关闭评论']",
          "[role='switch'][aria-label='允许评论']",
          "[role='checkbox'][aria-label='允许评论']",
          "[role='switch'][aria-label='关闭评论']",
          "[role='checkbox'][aria-label='关闭评论']",
        ],
        names: ['允许评论', '关闭评论'],
      },
      self_declaration: {
        type: 'enum',
        triggerNames: ['请选择自主声明'],
        options: {
          none: {
            names: ['无需添加自主声明'],
            confirmNames: ['确定'],
            selectedTextSelectors: ['.selectText-XSrMFZ'],
            selectedTextValues: ['无需添加自主声明'],
          },
          ai: {
            names: ['内容由AI生成'],
            confirmNames: ['确定'],
            selectedTextSelectors: ['.selectText-XSrMFZ'],
            selectedTextValues: ['内容由AI生成'],
          },
        },
      },
    },
    scheduleControl: {
      toggleNames: ['定时发布'],
      immediateNames: ['立即发布'],
      requireCheckedChoice: true,
      dateTimeFormat: 'yyyy-MM-dd HH:mm',
      dateTimeInputs: ["input[type='text'][placeholder='日期和时间'][format='yyyy-MM-dd HH:mm']"],
    },
  },
  kuaishou: {
    composerUrl: 'https://cp.kuaishou.com/article/publish/video',
    composerURLs: {
      graph_text: 'https://cp.kuaishou.com/article/publish/video',
    },
    // The live console currently maps tabType=3 to panoramic video. Select the
    // reviewed native image tab by its exact visible label in the same page.
    contentTabNames: {
      graph_text: ['上传图文'],
    },
    resumeDraftNames: ['继续编辑'],
    title: ["input[placeholder*='标题']"],
    titleOptional: true,
    body: ["textarea[placeholder*='描述']", "div[contenteditable='true']"],
    bodyWhitespaceEquivalent: true,
	// The creator page replaces the upload trigger with this native control
	// once its video preview is ready. Some previews are rendered without a
	// readable media `readyState`, so use the reviewed completion control only
	// together with the global progress/error checks below.
	uploadReadyNames: ['重新上传'],
	uploadStatusPageWide: true,
	uploadCompletionStableMs: 1500,
	requireUploadCompletionAtPrepare: true,
	uploadCompletionTimeoutMs: 90000,
    // The native Kuaishou video editor does not expose a comments or
    // declaration control for every account. Absence is acceptable only after
    // the complete reviewed settings surface is visible and its existing
    // controls have been independently observed.
    verifiedAbsentDefaults: true,
    verifiedAbsentComposer: 'kuaishou',
    topicControl: {
      mode: 'kuaishou_adaptive',
      optional: true,
      triggerNames: ['智能话题'],
      inlineTriggerNames: ['#话题'],
      recommendationSelectors: ["[class*='_ai-topics-item_']", "[class*='_ai-caption-item_']"],
      // The live assistant states that recommendations can take about 20s.
      // Keep the wait bounded, then fall back to native inline entry.
      settleMs: 25000,
      maxTopics: 3,
      allowRecommendationFallback: true,
    },
    dismissNames: ['Skip', '跳过'],
    mediaUploadTriggerNames: {
      image: ['上传图片'],
      video: ['上传视频', '重新上传'],
    },
	// A retained creator draft has a hidden stale input. Use the visible native
	// re-upload action so Kuaishou clears that draft before accepting the frozen
	// video; direct input writes would leave the preview-count proof unchanged.
	preferMediaUploadTriggerKinds: ['video'],
    clearExistingMediaNames: ['清空并重新上传'],
    coverControl: {
      triggerNames: ['选择封面', '设置封面'],
      panelMarkerNames: ['封面设置', '设置竖封面'],
      confirmNames: ['完成', '确认'],
      postConfirmNames: ['暂不设置', '我知道了'],
    },
    submitNames: ['发布', '立即发布'],
    // The current Kuaishou terminal control exposes exact text but no button
    // role. Keep this platform-specific and exact so unrelated publish copy
    // cannot be mistaken for the final review control.
    submitTextNames: ['发布'],
    // “作品管理” is a persistent navigation item in the composer, not a
    // post-submit acknowledgement. Treating it as one would fabricate a
    // success signal while the editor is still on screen.
    successText: ['发布成功'],
    // A trusted click only proves dispatch. This console keeps the composer
    // visible for unsaved / rejected submits, so require its explicit toast
    // before progressing to creator-backend verification.
    requirePostSubmitSignal: true,
    settingControls: {
      link_applet: { type: 'text', selectors: ["input[placeholder*='小程序']", "input[placeholder*='链接']"], triggerNames: ['关联小程序'], chooseExact: true },
      declaration: {
        type: 'enum', triggerNames: ['作品声明'], noOpValues: ['none'], verifiedAbsentValue: 'ai',
        options: {
          ai: { names: ['内容为AI生成', '内容为 AI 生成'] },
          fiction: { names: ['演绎情节仅供娱乐'] },
          opinion: { names: ['个人观点仅供参考'] },
          network: { names: ['素材来源于网络'] },
        },
      },
      collection: { type: 'text', selectors: ["input[placeholder*='合集']"], triggerNames: ['添加合集', '合集'], chooseExact: true },
      location: {
        type: 'text',
        selectors: [
          ".ant-select:has(.ant-select-selection-placeholder:has-text('请输入视频详细地址')) input.ant-select-selection-search-input",
          "[class*='location' i] input",
          "[class*='position' i] input",
          "input[placeholder*='详细地址']",
          "input[placeholder*='位置']",
          "input[placeholder*='地点']",
        ],
        regionTriggerSelectors: ['.ant-cascader'],
        regionTriggerNames: ['请选择所在地区'],
        regionOptionSelectors: [".ant-cascader-menu-item[title='杭州市']"],
        regionOptionNames: ['杭州市'],
        triggerNames: ['添加位置', '位置'],
        optionSelectors: [
          '.ant-select-item-option',
          "[role='listbox'] [role='option']",
          "[role='option']",
          "[class*='location' i] [class*='item' i]",
          "[class*='position' i] [class*='item' i]",
        ],
        selectedTextSelectors: [
          '.ant-select-selection-item',
        ],
        selectedTextAncestorXPath: "ancestor::*[contains(concat(' ', normalize-space(@class), ' '), ' ant-select ')][1]",
        chooseFuzzy: true,
        chooseFirstResult: true,
        minimumFuzzyTokenMatches: 2,
      },
      allow_duet: { type: 'boolean', noOpValues: [true], selectors: ["[role='switch'][aria-label*='同框']", "[role='checkbox'][aria-label*='同框']"], names: ['允许别人跟我拍同框'] },
      same_city: { type: 'boolean', noOpValues: [true], selectors: ["[role='switch'][aria-label*='同城']", "[role='checkbox'][aria-label*='同城']"], names: ['作品展示在同城页'] },
      visibility: {
        type: 'enum',
        options: {
          public: { names: ['所有人可见'] },
          friends: { names: ['好友可见', '好友'] },
          private: { names: ['仅自己可见', '私密'] },
        },
      },
      allow_download: { type: 'boolean', noOpValues: [true], selectors: ["[role='switch'][aria-label*='下载']", "[role='checkbox'][aria-label*='下载']"], names: ['允许下载', '允许下载此作品'] },
      comment_enabled: {
        type: 'boolean', verifiedAbsentValue: true,
        selectors: ["[role='switch'][aria-label*='评论']", "[role='checkbox'][aria-label*='评论']", "input[type='checkbox'][aria-label*='评论']"],
        names: ['允许评论', '关闭评论'],
      },
      music: { type: 'text', selectors: ["input[placeholder*='音乐']"], triggerNames: ['选择音乐', '音乐'], chooseExact: true },
    },
    scheduleControl: {
      toggleNames: ['定时发布'],
      // The official picker is a text-backed native control. If direct fill
      // is rejected, type into the focused control and require its readback.
      nativeTyping: true,
      antDateTimePicker: true,
      dateTimeInputs: ["input[type='datetime-local']", "input[placeholder='选择发布时间']", "input[placeholder='选择日期时间']", "input[aria-label='发布时间']"],
      dateInputs: ["input[type='date']", "input[placeholder='选择日期']"],
      timeInputs: ["input[type='time']", "input[placeholder='选择时间']"],
    },
  },
  wechat_video: {
    composerUrl: 'https://channels.weixin.qq.com/platform/post/create',
    editorFrameURLPrefixes: ['https://channels.weixin.qq.com/micro/content/post/create'],
    // The iframe controls appear only after the uploaded video is processed.
    editorReadyTimeoutMs: 60000,
    bodyNativeTyping: true,
    title: ["input[placeholder*='标题']"],
    body: [".input-editor[data-placeholder='添加描述']", "div[contenteditable='true']", "textarea[placeholder*='描述']"],
    topicControl: {
      mode: 'plain_space',
      commitKey: 'Space',
      settleMs: 1500,
      maxTopics: 3,
    },
    // Live creator-page evidence: the reviewed post-upload iframe exposes one
    // primary “发表” button. Strict selector + exact text avoids matching
    // “查看发表规范” and never authorizes clicking it during prepare.
    submitSelectors: ['button.weui-desktop-btn_primary:has-text("发表")'],
    strictSubmitSelectors: true,
    submitNames: ['发表', '发布'],
    // “内容管理” is always visible in the creator sidebar and cannot prove
    // that the final click was accepted.
    successText: ['发表成功', '发布成功'],
    capturePostSubmitEvidence: true,
    postSubmitConfirmation: {
      rootSelectors: ["[role='dialog'] .weui-desktop-dialog", '.weui-desktop-dialog'],
      noDeclarationNames: ['无需声明', '无需添加声明', '无需添加自主声明'],
      finalNames: ['确认发表', '确定发表', '继续发表', '发表', '发布'],
      timeoutMs: 3000,
    },
    settingControls: {
      location: {
        type: 'text',
        noOpValues: ['auto'],
        emptyReadback: {
          names: ['不展示位置', '不显示位置', '不选择位置'],
        },
        emptyChoiceNames: ['不展示位置', '不显示位置', '不选择位置'],
        selectors: [
          ".post-position-wrap input",
          "[class*='position' i] input",
          "input[placeholder*='位置']",
          "input[placeholder*='地点']",
          "[placeholder*='搜索地点']",
          "[data-placeholder*='搜索地点']",
        ],
        triggerSelectors: ['.position-display-wrap'],
        triggerNames: ['地理位置', '位置'],
        optionSelectors: [
          '.post-position-wrap .option-item',
          "[role='listbox'] [role='option']",
          "[role='option']",
          "[class*='position' i] [class*='item' i]",
          "[class*='location' i] [class*='item' i]",
        ],
        selectedTextAncestorXPath: 'ancestor::*[contains(concat(" ", normalize-space(@class), " "), " post-position-wrap ")][1]',
        selectedTextSelectors: ['.position-display-wrap .location-name'],
        selectedTextMustMatchValue: true,
        selectedTextMinimumFuzzyTokenMatches: 2,
        requireSelectedTextCommit: true,
        chooseFuzzy: true,
        chooseFirstResult: true,
        minimumFuzzyTokenMatches: 2,
      },
      collection: {
        type: 'text',
        selectors: ["input[placeholder*='合集']"],
        triggerNames: ['添加合集', '合集'],
        chooseExact: true,
        emptyReadback: { names: ['选择合集', '不选择合集', '未选择合集'] },
      },
      link_type: {
        type: 'enum', triggerNames: ['扩展链接', '链接类型'], noOpValues: ['none'],
        emptyReadback: { names: ['选择链接', '不设置链接', '未选择链接'] },
        options: { article: { names: ['公众号文章'] }, red_packet: { names: ['红包封面'] } },
      },
      link_address: {
        type: 'text',
        selectors: ["input[placeholder*='链接']", "input[placeholder*='文章']"],
        triggerNames: ['扩展链接', '链接地址'],
        chooseExact: true,
        parentSetting: { key: 'link_type', enabledValue: 'article' },
      },
      activity: {
        type: 'text',
        selectors: ["input[placeholder*='活动']"],
        triggerNames: ['参与活动'],
        chooseExact: true,
        emptyReadback: { names: ['不参与活动'] },
      },
      declare_original: {
        type: 'boolean',
        noOpValues: [false],
        selectors: [
          "[role='switch'][aria-label*='原创']",
          "[role='checkbox'][aria-label*='原创']",
          "input[type='checkbox'][name*='original' i]",
        ],
        checkedSelectors: ["input[type='checkbox']"],
        names: ['声明原创'],
        nearbyLabelNames: ['声明原创'],
        nearbyStateSelector: "input[type='checkbox'],[role='checkbox'][aria-checked]",
      },
      music: { type: 'text', selectors: ["input[placeholder*='音乐']"], triggerNames: ['选择音乐', '音乐'], chooseExact: true },
    },
    scheduleControl: {
      toggleNames: ['定时发表', '定时发布'],
      nearbyLabelNames: ['定时'],
      nearbyStateSelector: "input[type='radio']",
      requireCheckedChoice: true,
      dateTimeNearbyLabelNames: ['发表时间'],
      dateTimeNearbySelector: 'input',
      dateTimeFormat: 'yyyy-MM-dd HH:mm',
      wechatVideoDateTimePicker: true,
      nativeTyping: true,
      dateTimeInputs: ["input[type='datetime-local']", "input[placeholder='选择发表时间']", "input[placeholder='选择发布时间']"],
      dateInputs: ["input[type='date']", "input[placeholder='选择日期']"],
      timeInputs: ["input[type='time']", "input[placeholder='选择时间']"],
    },
  },
  wechat_mp: {
    composerUrl: '',
    requiredURLPrefixes: ['https://mp.weixin.qq.com/cgi-bin/appmsg'],
    // A user can have unrelated tabs in the account profile. Only an existing
    // WeChat creator page may be used to open the article editor; no fallback
    // to the context's last page is safe here.
    entryURLPrefixes: ['https://mp.weixin.qq.com/'],
    // Never overwrite an arbitrary existing draft. When the creator home is
    // open, start a fresh editor from that reviewed entry and scope all later
    // work to the popup created by this prepare action.
    preferEntryPage: true,
    entryNames: ['文章', '图文消息', '写新图文'],
    entrySelectors: ['.new-creation__menu-item:has-text("文章")'],
    title: [".title-editor__input .ProseMirror[contenteditable='true']", "textarea[placeholder*='标题']", "input[placeholder*='标题']"],
    // Do not fall through to arbitrary rich-text controls or iframes. A
    // selector drift must stop at manual review rather than write a title,
    // summary, or embedded third-party control as the article body.
    body: [".mock-iframe-body .ProseMirror[contenteditable='true']"],
    bodyWhitespaceEquivalent: true,
    // Frozen article tags remain in the reviewed article body. The current
    // official editor has no mandatory native topic picker for this flow.
    nativeTagsOptional: true,
    // WeChat inserts a selected image into the article editor. Uploading
    // before filling the ProseMirror body would immediately erase that image.
    uploadMediaAfterText: true,
    mediaUploadTriggerSelectors: {
      image: ['li.jsInsertIcon.img'],
    },
    requireArticleBodyImage: true,
    articleCoverControl: {
      // The reviewed WeChat editor exposes only “从正文选择” for this flow.
      // An explicit role=cover image may therefore be inserted into the body
      // solely as the platform's cover source; other platforms must never
      // inherit this exception.
      source: 'body_image',
      triggerSelectors: ['.js_cover_btn_area'],
      choiceOpenRootSelectors: ['.js_cover_opr[style*="display: block"]'],
      choiceRootSelectors: ['.js_cover_opr'],
      choiceSelectors: ['.js_selectCoverFromContent'],
      choiceNames: ['从正文选择'],
      candidateSelectors: ['.appmsg_content_img_item'],
      nextNames: ['下一步'],
      panelSelectors: ["[role='dialog']", '.weui-desktop-dialog'],
      confirmNames: ['确认', '确定', '完成'],
      committedSelectors: ['.js_cover_preview_new[style*="background-image"]'],
      choiceRetryAttempts: 2,
      choiceRetryDelayMs: 5000,
      retryEmptyPickerOnce: true,
      retryUncommittedOnce: true,
      // The official editor closes the cropper before its async cover write
      // becomes visible in the article form. Waiting is side-effect free;
      // do not reopen or re-click the cropper while that write is pending.
      commitTimeoutMs: 30000,
    },
    // The editor is opened from an existing creator tab. Do not spend the
    // request's entire timeout polling a stale or partially loaded popup.
    // A human can recover it from manual takeover once this bounded check
    // fails; no review control may be opened before title and body are ready.
    editorReadyTimeoutMs: 5000,
    advanceSelectors: ['button.mass_send'],
    advanceNames: ['发表'],
    submitSelectors: [
      "[role='dialog'] button.weui-desktop-btn_primary:has-text('无需声明并发表')",
      "[role='dialog'] button.weui-desktop-btn_primary:has-text('继续发表')",
    ],
    intermediateSubmitControl: {
      rootSelectors: ['.new_mass_send_dialog .weui-desktop-dialog'],
      submitSelectors: ['button.weui-desktop-btn_primary:has-text("发表")'],
      submitNames: ['发表'],
      booleanOptions: [
        { label: '群发通知', expected: false },
        { label: '分组通知', expected: false, absentWhenExpectedFalse: true },
        { label: '定时发表', expectedFromPublishAt: true },
      ],
      settingsApplied: ['publish_type'],
      readyTimeoutMs: 5000,
      finalTimeoutMs: 10000,
    },
    reopenIntermediateAtSubmit: true,
    submitTextNames: ['无需声明并发表', '继续发表'],
    strictSubmitSelectors: true,
    submitNames: ['发表', '群发'],
    successText: ['发表成功', '群发成功', '已发表'],
    platformVerificationTexts: ['扫码后，请联系管理员进行验证', '微信验证'],
    settingControls: {
      author: { type: 'text', selectors: ["input[placeholder*='作者']", "textarea[placeholder*='作者']"] },
      original_link: { type: 'text', selectors: ["input[placeholder*='原文链接']", "input[placeholder*='链接']"] },
      comment_enabled: { type: 'boolean', selectors: ['input.js_interaction_setting', "[role='checkbox'][aria-label*='留言']", "[role='switch'][aria-label*='留言']"], names: ['开启留言', '留言'] },
      declare_original: {
        type: 'boolean',
        selectors: ['.js_edit_ori input.js_ori_setting_checkbox', "[role='checkbox'][aria-label*='原创']", "[role='switch'][aria-label*='原创']"],
        names: ['声明原创'],
        booleanReadback: {
          selectors: ['.js_edit_ori .js_unset_original_title'],
          trueValues: ['已声明'],
          falseValues: ['未声明'],
        },
      },
      allow_reprint: {
        type: 'boolean',
        parentSetting: { key: 'declare_original', enabledValue: true, absentWhenDisabled: true },
        selectors: ["[role='checkbox'][aria-label*='转载']", "[role='switch'][aria-label*='转载']"],
        names: ['允许快捷转载', '快捷转载'],
      },
      publish_type: {
        type: 'enum', noOpValues: ['mass'],
        options: { mass: { names: ['群发'] }, publish: { names: ['发布'] } },
      },
      collection: {
        type: 'text', selectors: ["input[placeholder*='合集']"], triggerNames: ['合集'], chooseExact: true,
        emptyReadback: { selectors: ['.js_article_tags_content'], values: ['未添加'] },
      },
      content_source: {
        type: 'enum', triggerNames: ['内容来源声明'], noOpValues: ['none'],
        emptyReadback: { selectors: ['.js_claim_source_selected'], values: ['无需声明'] },
        options: {
          ai: { names: ['内容由AI生成', '内容由 AI 生成'] }, official_media: { names: ['素材来源官方媒体/网络新闻'] },
          fiction: { names: ['内容剧情演绎仅供娱乐'] }, opinion: { names: ['个人观点仅供参考'] },
          health: { names: ['健康医疗分享仅供参考'] }, investment: { names: ['投资观点仅供参考'] },
        },
      },
    },
    scheduleControl: {
      nearbyLabelNames: ['定时发表'],
      nearbyStateSelector: "input[type='checkbox']",
      requireCheckedChoice: true,
      wechatMPDateTimePicker: true,
    },
  },
  csdn: {
    composerUrl: 'https://editor.csdn.net/md/',
    titleActivationNames: ['【无标题】', '[无标题]', '无标题'],
    // Saved CSDN drafts render their current title as a display div until it
    // is clicked. The placeholder-only path works only for a fresh draft.
    titleActivationSelectors: ['.article-bar__title-display'],
    title: ["input[placeholder*='标题']", "textarea[placeholder*='标题']", '#txtTitle'],
    // Current CSDN editor evidence is its cledit pre element. Do not fall
    // through to arbitrary editable fields (for example a summary or tag
    // search input): a selector drift must require human review, not fill an
    // unrelated control and claim success.
    body: ["pre.editor__inner[contenteditable='true']", ".CodeMirror textarea"],
    // CSDN has two distinct publish stages. The editor button only opens the
    // options dialog; only the red button inside that dialog is the final
    // submit control. Keep these selectors strict so the first stage can
    // never be mistaken for ready-to-publish evidence.
    // The first-stage editor action is never a red terminal control. Exclude
    // the reviewed final-button class even when a platform style update makes
    // it share the generic editor button classes.
    advanceSelectors: ['button.btn.btn-publish:not(.btn-b-red)'],
    // CSDN's reviewed final button is not consistently wrapped by the old
    // modal class. Match the exact final control, but never click it in
    // prepare mode (CSDN submit remains blocked by validateInput).
    submitSelectors: ['button.btn-b-red:has-text("发布文章")'],
    strictSubmitSelectors: true,
    // The editor also has a red “发布文章” action. Its text/class alone is
    // insufficient: the terminal control exists only while this reviewed
    // publish-options surface is visible.
    optionsDialogSelectors: ['.publish-article-modal__body'],
    // Cropper teardown briefly leaves the editor action inert. Retrying this
    // non-terminal transition is safe only while the reviewed options dialog
    // and its final control remain absent.
    advanceRetryAttempts: 2,
    separateTagControl: true,
    tagsRequired: true,
    tagTriggerNames: ['添加文章标签'],
    tagInputSelectors: ["input[placeholder='请输入文字搜索，Enter键入可添加自定义标签']"],
    // CSDN's tag picker has an exact close control. Escape is intentionally
    // not used: the editor also binds it to dismiss the surrounding publish
    // options dialog, which drops a just-uploaded cover from the review form.
    tagPickerDismissWithCloseControl: true,
    tagPickerCloseSelectors: [".mark_selection_box button[aria-label='关闭']"],
    submitNames: ['发布文章', '发布博客'],
    successText: ['发布成功', '审核中', '文章管理'],
    // CSDN can keep its final dialog open after rejecting an otherwise trusted
    // click. Without an explicit post-click state, that click is irreversible
    // but its outcome is unknown and must never be queued as publication.
    requirePostSubmitSignal: true,
    articleCoverControl: {
      // CSDN exposes its article cover only after the non-terminal publish
      // options dialog opens. The hidden input is still selected directly by
      // Playwright and must prove both frozen-file selection and preview.
      source: 'native_upload', afterOptions: true,
      // The CSDN tag and timer dialogs cover the native cover target. Finish
      // its proof before opening any optional form overlays.
      beforeSeparateTags: true,
      // The file input is wired only after the visible “从本地上传” action.
      // Selecting a stale hidden input is not evidence of a cover upload.
      triggerSelectors: ['.publish-article-modal__body .upload-img-box'],
      // CSDN currently mounts either of these inputs beneath the same native
      // upload action. They are alternatives, never a combined multi-file
      // target: each must independently retain the frozen file and render a
      // preview before the next is considered.
      uploadSelectorGroups: [
        ['.publish-article-modal__body input.cfw-file-input'],
        ['.publish-article-modal__body input.el-upload__input'],
      ],
      // CSDN can render an accepted cover as a background instead of an img.
      // `preview-box` always contains an empty img. CSDN only marks an
      // uploaded direct_blog_coverimage as `hasImg`, so neither that empty
      // img nor the unrelated body-image selection area proves a cover.
      committedSelectors: [
        '.publish-article-modal__body .preview-box.hasImg img.preview[src]:not([src=""])',
      ],
      // The platform stages a selected local image in its own editor modal.
      // That preview is not yet the article cover until this exact native
      // confirmation is clicked and the publish-options preview commits.
      nativeUploadConfirmNames: ['确认上传'],
      nativeUploadConfirmSelectors: ['.vue-image-crop-upload .vicp-operate-btn'],
      // CSDN starts `direct_blog_coverimage` after the crop dialog closes.
      // It is an authenticated native upload and can settle after the dialog
      // itself has disappeared, so retain the single controlled page long
      // enough to observe the real `preview-box.hasImg` state.
      commitTimeoutMs: 20000,
      // CSDN closes the first publish-options modal when the cropper commits.
      // Reopen only that non-terminal stage before tags/settings/final-button
      // readback; never reselect or upload the frozen cover.
      reopenOptionsAfterCommit: true,
    },
    settingControls: {
      collection: { type: 'text', noOpValues: [''], selectors: ["input[placeholder*='专栏']"], triggerNames: ['分类专栏', '添加专栏'], chooseExact: true },
      article_type: {
        type: 'enum', noOpValues: ['original'],
        options: {
          original: { names: ['原创'], selectors: ['label[for="original"]'], checkedSelectors: ['#original'] },
          repost: { names: ['转载'], selectors: ['label[for="repost"]'], checkedSelectors: ['#repost'] },
          translation: { names: ['翻译'], selectors: ['label[for="translated"]'], checkedSelectors: ['#translated'] },
        },
      },
      original_link: { type: 'text', selectors: ["input[placeholder*='原文链接']", "input[placeholder*='文章链接']"] },
      backup_gitcode: { type: 'boolean', noOpValues: [false], selectors: ['.publish-article-modal__body .gitcode-box label.el-checkbox'], checkedSelectors: ['.publish-article-modal__body .gitcode-box input[type="checkbox"]'], names: ['同时备份到 GitCode'] },
      visibility: {
        type: 'enum', triggerNames: ['可见范围'], noOpValues: ['public'],
        options: {
          public: { names: ['全部可见', '公开'], selectors: ['label[for="public"]'], checkedSelectors: ['#public'] }, private: { names: ['仅自己可见', '仅自己'], selectors: ['label[for="private"]'], checkedSelectors: ['#private'] },
          followers: { names: ['粉丝可见'], selectors: ['label[for="needfans"]'], checkedSelectors: ['#needfans'] }, vip: { names: ['VIP可见', 'VIP 可见'], selectors: ['label[for="needvip"]'], checkedSelectors: ['#needvip'] },
        },
      },
      activity: { type: 'text', selectors: ["input[placeholder*='活动']"], triggerNames: ['参与活动'], chooseExact: true },
      topic: { type: 'text', selectors: ["input[placeholder*='话题']"], triggerNames: ['话题'], chooseExact: true },
    },
    scheduleControl: {
      toggleNames: ['定时发布'],
      dateTimeInputs: ["input[type='datetime-local']", "input[placeholder='选择发布时间']", "input[aria-label='发布时间']"],
      dateInputs: ["input[type='date']", "input[placeholder='选择日期']"],
      timeInputs: ["input[type='time']", "input[placeholder='选择时间']"],
      elementDateTimePicker: {
        dateSelector: '.publish-timer-dialog .txt-date:not(.ml8) input.el-input__inner',
        timeSelector: '.publish-timer-dialog .txt-date.ml8 input.el-input__inner',
        minuteStep: 15,
      },
    },
  },
  youtube: {
    // YouTube Studio exposes a reviewed “Upload videos” entry on its home
    // surface. The channel ID is account-specific, so never guess a direct
    // /channel/{id}/videos/upload URL.
    entryFlows: {
      video: {
        entryURL: 'https://studio.youtube.com/',
        entryNames: ['Upload videos', '上传视频'],
        requiredURLPrefixes: ['https://studio.youtube.com/'],
      },
    },
    postUploadSettleMs: 1000,
    uploadRootSelector: '.dialog-content.style-scope.ytcp-uploads-dialog',
    uploadStatusPageWide: true,
    requireUploadCompletionAtPrepare: true,
    uploadCompletionTimeoutMs: 90000,
    uploadCompletionStableMs: 1500,
    title: ['#title-textarea #textbox', "ytcp-social-suggestions-textbox[id='title-textarea'] #textbox"],
    body: ['#description-textarea #textbox', "ytcp-social-suggestions-textbox[id='description-textarea'] #textbox"],
    bodyOptional: true,
    mediaUploadTriggerNames: { video: ['Select files', '选择文件'] },
    separateTagControl: true,
    tagsBeforeAdvance: true,
    tagTriggerNames: ['Show more', '显示更多'],
    tagInputSelectors: ["input[aria-label='Tags']", "input[aria-label='标签']", "#tags-container input"],
    coverControl: {
      triggerNames: ['Upload thumbnail', '上传缩略图'],
      requireExplicitUpload: true,
      allowNativeDefaultWhenExplicitUnavailable: true,
      nativeDefaultNames: [
        'You can change the thumbnail in the YouTube mobile app',
        '你可以在 YouTube 移动应用中更改缩略图',
      ],
      directFileChooser: true,
      triggerTimeoutMs: 90000,
      committedPreviewSelectors: [
        'ytcp-video-metadata-editor-thumb img[src]',
        '#thumbnail-editor img[src]',
        'ytcp-video-metadata-editor-thumb #thumbnail-container',
        'ytcp-video-metadata-editor-thumb [style*="background-image"]',
      ],
    },
    coverPreviewMayUnmountAfterAdvance: true,
    settingControls: {
      audience: {
        type: 'enum',
        options: {
          not_made_for_kids: { names: ["No, it's not made for kids", '否，它不是面向儿童的内容'] },
          made_for_kids: { names: ["Yes, it's made for kids", '是，它是面向儿童的内容'] },
        },
      },
      visibility: {
        type: 'enum',
        options: {
          private: { names: ['Private', '私享'] },
          unlisted: { names: ['Unlisted', '不公开列出'] },
          public: { names: ['Public', '公开'] },
        },
      },
      playlist: {
        type: 'text',
        selectors: ["input[aria-label*='playlist' i]", "input[placeholder*='playlist' i]"],
        triggerNames: ['Select', '选择'],
        chooseExact: true,
        emptyReadback: {
          selectors: [
            'ytcp-video-metadata-playlists ytcp-dropdown-trigger',
            'ytcp-video-metadata-playlists #dropdown-trigger',
            '#playlists-container #dropdown-trigger',
          ],
          values: ['Select', '选择'],
        },
      },
      allow_comments: {
        type: 'enum',
        options: {
          true: {
            selectedTextSelectors: ['#enablement-state-select ytcp-dropdown-trigger'],
            selectedTextValues: ['Comments On', '评论 开启'],
            names: ['On', '开启'],
          },
          false: {
            selectedTextSelectors: ['#enablement-state-select ytcp-dropdown-trigger'],
            selectedTextValues: ['Comments Off', '评论 关闭'],
            names: ['Off', '关闭'],
          },
        },
      },
      altered_content: {
        type: 'enum',
        options: {
          true: {
            selectors: ["[role='radio'][aria-label='Yes, AI was used']"],
            names: ['Yes, AI was used'],
          },
          false: {
            selectors: ["[role='radio'][aria-label='No, AI wasn’t used']"],
            names: ['No, AI wasn’t used'],
          },
        },
      },
    },
    advanceCount: 3,
    advanceNames: ['Next', '下一步'],
    postAdvanceDismissNames: ['Next', '下一步', 'Got it', '知道了', 'Done', '完成'],
    submitNames: ['Save', '保存', 'Publish', '发布', 'Schedule', '定时发布'],
    platformVerificationTexts: [
      'We’re still checking your content',
      "We're still checking your content",
      '我们仍在检查你的内容',
    ],
    // YouTube disables Save while the uploaded video is being processed.
    // Wait only for this reviewed final control; never click it in prepare mode.
    submitReadyTimeoutMs: 90000,
    successText: ['Video published', '视频已发布', 'Checks complete', '检查完毕'],
    scheduleControl: {
      toggleNames: ['Schedule', '定时发布'],
      requireCheckedChoice: true,
      selectedStateSelectors: [
        'ytcp-visibility-scheduler ytcp-datetime-picker',
      ],
      youtubeDateTimePicker: {
        settleMs: 1000,
        // YouTube can keep metadata in Saving... for the full initial upload
        // processing window. Editing earlier is accepted briefly, then reset.
        autosaveTimeoutMs: 10 * 60 * 1000,
        dateTriggerSelectors: [
          'ytcp-datetime-picker ytcp-text-dropdown-trigger',
        ],
        timeInputSelectors: [
          'input#time-of-day',
          '#time-of-day input',
          'ytcp-datetime-picker form input',
          'ytcp-datetime-picker tp-yt-paper-input input',
        ],
      },
      dateTimeInputs: ["input[type='datetime-local']", "input[aria-label*='schedule' i]"],
      dateInputs: ["input[type='date']", "input[aria-label*='date' i]"],
      timeInputs: ["input[type='time']", "input[aria-label*='time' i]"],
    },
  },
  tiktok: {
    composerUrl: 'https://www.tiktok.com/tiktokstudio/upload',
    preUploadTransientErrorNames: ['出错了', 'Something went wrong', 'Something went wrong.'],
    preUploadTransientRetryNames: ['重试', 'Retry'],
    preUploadTransientDetectionMs: 5000,
    preUploadTransientFallbackURL: 'https://www.tiktok.com/upload?lang=en',
    verifiedAbsentDefaults: true,
    verifiedAbsentComposer: 'tiktok_video',
    composerURLs: {
      video: 'https://www.tiktok.com/tiktokstudio/upload',
      graph_text: 'https://www.tiktok.com/tiktokstudio/upload?from=webapp',
    },
    composerURLPrefixes: {
      video: [
        'https://www.tiktok.com/tiktokstudio/upload',
        'https://www.tiktok.com/upload',
      ],
    },
    postUploadSettleMs: 1000,
    staleDraftPromptNames: [
      'Past edited video was not saved. Continue editing?',
      '过往编辑的视频未保存。继续编辑?',
    ],
    staleDraftDiscardNames: ['Discard', '放弃'],
    postUploadDismissNames: ['Got it', '知道了'],
    postUploadCancelDialogNames: ['Cancel', '取消'],
    title: ["input[placeholder*='title' i]", "input[placeholder*='标题']"],
    titleOptional: true,
    body: [
      "div.public-DraftEditor-content[contenteditable='true']",
      "div[contenteditable='true'][data-placeholder]",
      "div[contenteditable='true'][aria-label*='caption' i]",
      "textarea[placeholder*='caption' i]",
      "textarea[placeholder*='描述']",
    ],
    bodyWhitespaceEquivalent: true,
    topicControl: {
      mode: 'trigger_picker',
      triggerNames: ['Hashtags', '话题标签'],
      commitKey: 'Space',
      preserveNativeFocus: true,
      requireTriggerMarker: true,
      settleMs: 1500,
      maxTopics: 5,
    },
    mediaUploadTriggerNames: {
      video: ['Select video', '选择视频', 'Select file', '选择文件'],
      image: ['Select photos', '选择图片', 'Upload photos', '上传图片'],
    },
    coverControl: {
      triggerNames: ['Edit cover', '编辑封面', 'Select cover', '选择封面'],
      confirmNames: ['Save', '保存', 'Confirm', '确认'],
      // TikTok Studio renders a native cover thumbnail immediately after the
      // frozen video upload. Its web editor does not expose a separate image
      // file input, so the reviewed Edit cover marker proves the native
      // first-frame cover exists without pretending the frozen PNG was sent.
      nativeDefaultNames: ['Edit cover', '编辑封面'],
    },
    coverAfterPostUploadDialogs: true,
    settingControls: {
      visibility: {
        type: 'enum',
        triggerSelectors: ['.view-auth-container .Select__trigger'],
        triggerNames: ['Everyone', '所有人', 'Who can watch', 'Who can view', '谁可以观看', '谁可以看'],
        options: {
          public: {
            names: ['Everyone', '所有人'],
            selectedTextSelectors: ['.view-auth-container .Select__trigger'],
            selectedTextValues: ['Everyone', '所有人'],
          },
          friends: {
            names: ['Friends', '好友'],
            selectedTextSelectors: ['.view-auth-container .Select__trigger'],
            selectedTextValues: ['Friends', '好友'],
          },
          private: {
            names: ['Only you', '仅自己'],
            selectedTextSelectors: ['.view-auth-container .Select__trigger'],
            selectedTextValues: ['Only you', '仅自己'],
          },
        },
      },
      allow_comment: {
        type: 'boolean',
        selectors: ["[role='switch'][aria-label*='comment' i]", "input[type='checkbox'][name*='comment' i]"],
        names: ['Allow comments', 'Comments', '允许评论', '评论'],
        nearbyLabelNames: ['Allow comments', 'Comments', '允许评论', '评论'],
        nearbyStateSelector: 'input.Checkbox__input',
      },
      // ponytail: TikTok Studio currently exposes one reviewed "Reuse content"
      // checkbox for both Duet and Stitch. Applying the same false state twice
      // is intentional; split controls remain supported by the older names.
      allow_duet: {
        type: 'boolean',
        selectors: ["[role='switch'][aria-label*='duet' i]", "input[type='checkbox'][name*='duet' i]"],
        names: ['Allow Duet', '允许合拍', 'Reuse content', '复用内容'],
        nearbyLabelNames: ['Allow Duet', '允许合拍', 'Reuse content', '复用内容'],
        nearbyStateSelector: 'input.Checkbox__input',
      },
      allow_stitch: {
        type: 'boolean',
        selectors: ["[role='switch'][aria-label*='stitch' i]", "input[type='checkbox'][name*='stitch' i]"],
        names: ['Allow Stitch', '允许拼接', 'Reuse content', '复用内容'],
        nearbyLabelNames: ['Allow Stitch', '允许拼接', 'Reuse content', '复用内容'],
        nearbyStateSelector: 'input.Checkbox__input',
      },
      ai_generated: {
        type: 'boolean',
        selectors: ["[role='switch'][aria-label*='AI-generated' i]", "input[type='checkbox'][name*='ai' i]"],
        names: ['AI-generated content', 'AI 生成内容', 'AI 生成的内容'],
        nearbyLabelNames: ['AI-generated content', 'AI 生成内容', 'AI 生成的内容'],
        nearbyStateSelector: 'input.Switch__input',
      },
      allow_download: {
        type: 'boolean',
        verifiedAbsentValue: false,
        selectors: ["[role='switch'][aria-label*='download' i]", "input[type='checkbox'][name*='download' i]"],
        names: ['Allow downloads', '允许下载'],
      },
    },
    submitNames: ['Post', '发布', 'Schedule', '预约发布'],
    successText: ['Your video is being uploaded', 'Your post is being uploaded', '上传中', '发布成功'],
    scheduleControl: {
      toggleNames: ['Schedule', '定时发布', '预约发布'],
      nearbyLabelNames: ['Schedule', '定时发布', '预约发布'],
      nearbyStateSelector: 'input.Radio__input',
      requireCheckedChoice: true,
      confirmNames: ['Allow', '允许'],
      tiktokDateTimePicker: {
        inputSelector: '.scheduled-picker input.TUXTextInputCore-input',
        calendarSelector: '.calendar-wrapper',
        daySelector: '.calendar-wrapper .day',
        hourSelector: '.tiktok-timepicker-option-text.tiktok-timepicker-left',
        minuteSelector: '.tiktok-timepicker-option-text.tiktok-timepicker-right',
        minuteStep: 5,
      },
      dateTimeInputs: ["input[type='datetime-local']", "input[aria-label*='schedule' i]"],
      dateInputs: ["input[type='date']", "input[placeholder*='date' i]"],
      timeInputs: ["input[type='time']", "input[placeholder*='time' i]"],
    },
  },
});

const PLATFORM_CODES = Object.freeze(Object.keys(PLATFORM_RULES));
const CDP_PORT_MIN = 15101;
const CDP_PORT_MAX = 15600;
const ALLOWED_INPUT_KEYS = Object.freeze([
  'schemaVersion', 'action', 'platform', 'cdpUrl', 'content', 'media', 'settings',
  'publishAt', 'allowSubmit', 'allowDelete', 'timeoutMs',
]);
const ALLOWED_CONTENT_KEYS = Object.freeze(['type', 'title', 'caption', 'body', 'tags']);
const ALLOWED_MEDIA_KEYS = Object.freeze(['path', 'kind', 'role', 'position', 'sha256']);
const ALLOWED_SETTINGS_KEYS = Object.freeze([
  'visibility', 'comment_enabled', 'declare_original', 'allow_download', 'allow_duet', 'allow_copy', 'allow_stitch',
  'location', 'collection', 'group', 'content_source', 'related_media', 'activity', 'extension_type',
  'hotspot', 'author', 'original_link', 'allow_reprint', 'publish_type', 'article_type',
  'backup_gitcode', 'topic', 'music', 'link_applet', 'declaration', 'same_city',
  'link_type', 'link_address', 'audience', 'playlist', 'allow_comments', 'altered_content',
  'allow_comment', 'ai_generated', 'self_declaration',
]);
const PLATFORM_SETTING_KEYS = Object.freeze({
  xhs: Object.freeze(['location', 'collection', 'group', 'declare_original', 'content_source', 'related_media', 'visibility', 'comment_enabled', 'allow_duet', 'allow_copy']),
  douyin: Object.freeze(['activity', 'collection', 'extension_type', 'location', 'hotspot', 'allow_download', 'visibility', 'music', 'comment_enabled', 'self_declaration']),
  kuaishou: Object.freeze(['link_applet', 'declaration', 'collection', 'location', 'allow_duet', 'allow_download', 'same_city', 'visibility', 'music', 'comment_enabled']),
  wechat_video: Object.freeze(['location', 'collection', 'link_type', 'link_address', 'activity', 'declare_original', 'music']),
  wechat_mp: Object.freeze(['author', 'original_link', 'comment_enabled', 'declare_original', 'allow_reprint', 'publish_type', 'collection', 'content_source']),
  csdn: Object.freeze(['collection', 'article_type', 'original_link', 'backup_gitcode', 'visibility', 'activity', 'topic']),
  youtube: Object.freeze(['audience', 'visibility', 'playlist', 'allow_comments', 'altered_content']),
  tiktok: Object.freeze(['visibility', 'allow_comment', 'allow_duet', 'allow_stitch', 'ai_generated', 'allow_download']),
});
const STRING_SETTING_KEYS = Object.freeze([
  'location', 'collection', 'group', 'related_media', 'activity', 'hotspot', 'author',
  'original_link', 'topic', 'music', 'link_applet', 'link_address', 'playlist',
]);
const PLATFORM_ENUM_SETTING_VALUES = Object.freeze({
  xhs: Object.freeze({
    visibility: Object.freeze(['public', 'friends', 'private']),
    content_source: Object.freeze(['none', 'fiction', 'ai', 'self_disclosed', 'original', 'reprint']),
  }),
  douyin: Object.freeze({
    visibility: Object.freeze(['public', 'friends', 'private']),
    extension_type: Object.freeze(['none', 'location', 'film_tv', 'mini_program', 'mark_objects']),
    self_declaration: Object.freeze(['none', 'ai']),
  }),
  wechat_mp: Object.freeze({
    publish_type: Object.freeze(['mass', 'publish']),
    content_source: Object.freeze(['none', 'ai', 'official_media', 'fiction', 'opinion', 'health', 'investment']),
  }),
  csdn: Object.freeze({
    article_type: Object.freeze(['original', 'repost', 'translation']),
    visibility: Object.freeze(['public', 'private', 'followers', 'vip']),
  }),
  kuaishou: Object.freeze({
    declaration: Object.freeze(['none', 'ai', 'fiction', 'opinion', 'network']),
    visibility: Object.freeze(['public', 'friends', 'private']),
  }),
  wechat_video: Object.freeze({ link_type: Object.freeze(['none', 'article', 'red_packet']) }),
  youtube: Object.freeze({
    audience: Object.freeze(['not_made_for_kids', 'made_for_kids']),
    visibility: Object.freeze(['private', 'unlisted', 'public']),
  }),
  tiktok: Object.freeze({ visibility: Object.freeze(['public', 'friends', 'private']) }),
});
const PLATFORM_CONTENT_TYPES = Object.freeze({
  xhs: Object.freeze(['graph_text', 'video']),
  douyin: Object.freeze(['graph_text', 'video']),
  kuaishou: Object.freeze(['graph_text', 'video']),
  wechat_video: Object.freeze(['video']),
  wechat_mp: Object.freeze(['article']),
  csdn: Object.freeze(['article']),
  youtube: Object.freeze(['video']),
  tiktok: Object.freeze(['video']),
});

function exactKeys(value, allowed, name) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`${name}_INVALID`);
  for (const key of Object.keys(value)) {
    if (!allowed.includes(key)) throw new Error(`${name}_UNKNOWN_FIELD`);
  }
}

function validateInput(input) {
  exactKeys(input, ALLOWED_INPUT_KEYS, 'INPUT');
  if (input.schemaVersion !== 1) throw new Error('SCHEMA_VERSION_UNSUPPORTED');
  if (!['inspect', 'inspect_settings', 'inspect_composer', 'inspect_manager', 'delete_content', 'resume_topics', 'prepare', 'submit', 'submit_prepared'].includes(input.action)) throw new Error('ACTION_UNSUPPORTED');
  if (!PLATFORM_CODES.includes(input.platform)) throw new Error('PLATFORM_UNSUPPORTED');
  let endpoint;
  try { endpoint = new URL(input.cdpUrl); } catch (_) { throw new Error('CDP_URL_INVALID'); }
  const port = Number(endpoint.port);
  if (endpoint.protocol !== 'http:' || endpoint.hostname !== '127.0.0.1' || port < CDP_PORT_MIN || port > CDP_PORT_MAX || endpoint.pathname !== '/') {
    throw new Error('CDP_URL_INVALID');
  }
  if (!Number.isInteger(input.timeoutMs) || input.timeoutMs < 1000 || input.timeoutMs > 600000) throw new Error('TIMEOUT_INVALID');
  if ((input.action === 'submit' || input.action === 'submit_prepared') && input.allowSubmit !== true) throw new Error('SUBMIT_APPROVAL_REQUIRED');
  if (input.action !== 'submit' && input.action !== 'submit_prepared' && input.allowSubmit === true) throw new Error('SUBMIT_FLAG_INVALID');
  if (input.action === 'delete_content' && input.allowDelete !== true) throw new Error('DELETE_APPROVAL_REQUIRED');
  if (input.action !== 'delete_content' && input.allowDelete === true) throw new Error('DELETE_FLAG_INVALID');
  // Inspection attaches to an existing page only. Publish-only validation of
  // media and settings would make a passive DOM inventory depend on files it
  // never reads, so stop here before any form-specific requirement.
  if (input.action === 'inspect' || input.action === 'inspect_settings' || input.action === 'inspect_composer' || input.action === 'inspect_manager') return input;
  if (input.publishAt !== undefined && (
    typeof input.publishAt !== 'string'
    || (input.publishAt !== '' && (
      !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(input.publishAt)
      || Number.isNaN(Date.parse(input.publishAt))
    ))
  )) throw new Error('PUBLISH_TIME_INVALID');
  exactKeys(input.content, ALLOWED_CONTENT_KEYS, 'CONTENT');
  if (!['graph_text', 'video', 'article'].includes(input.content.type)) throw new Error('CONTENT_TYPE_INVALID');
  if (!PLATFORM_CONTENT_TYPES[input.platform].includes(input.content.type)) throw new Error('CONTENT_TYPE_UNSUPPORTED');
  for (const key of ['title', 'caption', 'body']) {
    if (typeof input.content[key] !== 'string') throw new Error('CONTENT_INVALID');
  }
  if ((!input.content.title.trim() && !PLATFORM_RULES[input.platform].titleOptional)
    || (!(input.content.body || input.content.caption).trim() && !PLATFORM_RULES[input.platform].bodyOptional)) throw new Error('CONTENT_INVALID');
  if (!Array.isArray(input.content.tags) || input.content.tags.length > 20 || input.content.tags.some((tag) => (
    typeof tag !== 'string' || !tag.trim() || !tag.trim().replace(/^#+/, '').trim() ||
    tag.length > 64 || /[\r\n\0]/.test(tag)
  ))) throw new Error('TAGS_INVALID');
  if (!Array.isArray(input.media) || input.media.length > 10) throw new Error('MEDIA_INVALID');
  const positions = new Set();
  input.media.forEach((item) => {
    exactKeys(item, ALLOWED_MEDIA_KEYS, 'MEDIA_ITEM');
    if (typeof item.path !== 'string' || !['image', 'video'].includes(item.kind) || !['content', 'video', 'cover'].includes(item.role)) throw new Error('MEDIA_ITEM_INVALID');
    if (!Number.isInteger(item.position) || typeof item.sha256 !== 'string' || !/^[a-f0-9]{64}$/.test(item.sha256)) throw new Error('MEDIA_ITEM_INVALID');
    if (positions.has(item.position)) throw new Error('MEDIA_INVALID');
    positions.add(item.position);
  });
  exactKeys(input.settings, ALLOWED_SETTINGS_KEYS, 'SETTINGS');
  const supportedSettings = PLATFORM_SETTING_KEYS[input.platform];
  for (const [key, value] of Object.entries(input.settings)) {
    if (!supportedSettings.includes(key)) throw new Error('SETTINGS_UNSUPPORTED');
    const enumValues = PLATFORM_ENUM_SETTING_VALUES[input.platform]?.[key];
    if (enumValues) {
      if (!enumValues.includes(value)) throw new Error('SETTINGS_INVALID');
    } else if (STRING_SETTING_KEYS.includes(key)) {
      if (typeof value !== 'string' || value.length > 128 || /[\r\n\0]/.test(value)) throw new Error('SETTINGS_INVALID');
    } else if (typeof value !== 'boolean') {
      throw new Error('SETTINGS_INVALID');
    }
  }
  const contentMedia = input.media.filter((item) => item.role === 'content');
  const videoMedia = input.media.filter((item) => item.role === 'video');
  const coverMedia = input.media.filter((item) => item.role === 'cover');
  if (input.content.type === 'graph_text' && (
    contentMedia.length < 1 || contentMedia.length > 9 || videoMedia.length || coverMedia.length ||
    contentMedia.some((item) => item.kind !== 'image')
  )) throw new Error('MEDIA_INVALID');
  if (input.content.type === 'video' && (
    contentMedia.length || videoMedia.length !== 1 || videoMedia[0].kind !== 'video' || coverMedia.length > 1 ||
    coverMedia.some((item) => item.kind !== 'image')
  )) throw new Error('MEDIA_INVALID');
  if (input.content.type === 'article' && (
    contentMedia.length > 9 || videoMedia.length || coverMedia.length > 1 ||
    contentMedia.some((item) => item.kind !== 'image') || coverMedia.some((item) => item.kind !== 'image')
  )) throw new Error('MEDIA_INVALID');
  return input;
}

async function firstVisible(page, selectors) {
  for (const selector of selectors) {
    const matches = page.locator(selector);
    const count = Math.min(await matches.count(), 20);
    for (let index = 0; index < count; index += 1) {
      const locator = matches.nth(index);
      if (await locator.isVisible().catch(() => false)) return locator;
    }
  }
  return null;
}

async function uniqueVisible(page, selectors) {
  if (!selectors?.length) return { control: null, ambiguous: false };
  const matches = page.locator(selectors.join(','));
  const count = Math.min(await matches.count(), 21);
  let control = null;
  for (let index = 0; index < count; index += 1) {
    const candidate = matches.nth(index);
    if (!await candidate.isVisible().catch(() => false)) continue;
    if (control) return { control: null, ambiguous: true };
    control = candidate;
  }
  return { control, ambiguous: false };
}

async function uniqueVisibleNamedControl(page, names, roles) {
  let control = null;
  for (const name of names || []) {
    for (const role of roles) {
      const matches = page.getByRole(role, { name, exact: true });
      const count = Math.min(await matches.count(), 21);
      for (let index = 0; index < count; index += 1) {
        const candidate = matches.nth(index);
        if (!await candidate.isVisible().catch(() => false)) continue;
        if (control) return { control: null, ambiguous: true };
        control = candidate;
      }
    }
  }
  return { control, ambiguous: false };
}

async function firstVisibleEventually(page, selectors, timeoutMs) {
  if (!selectors.length) return null;
  const deadline = Date.now() + timeoutMs;
  do {
    const locator = await firstVisible(page, selectors);
    if (locator) return locator;
    await page.waitForTimeout(500);
  } while (Date.now() < deadline);
  return null;
}

async function clickNamed(page, names) {
  for (const name of names || []) {
    const exact = page.getByRole('button', { name, exact: true }).first();
    if (await exact.count() && await exact.isVisible().catch(() => false)) {
      await exact.click();
      return name;
    }
    const link = page.getByRole('link', { name, exact: true }).first();
    if (await link.count() && await link.isVisible().catch(() => false)) {
      await link.click();
      return name;
    }
    const text = page.getByText(name, { exact: true }).first();
    if (await text.count() && await text.isVisible().catch(() => false)) {
      await text.click();
      return name;
    }
  }
  return '';
}

async function clickNamedEventuallyExact(page, names, timeoutMs = 3000) {
  const deadline = Date.now() + timeoutMs;
  do {
    const clicked = await clickNamed(page, names).catch(() => '');
    if (clicked) return clicked;
    if (Date.now() >= deadline) break;
    await page.waitForTimeout(250);
  } while (true);
  return '';
}

async function clickNamedInDialog(page, names) {
  for (const name of names || []) {
    const candidates = page.getByRole('button', { name, exact: true });
    const count = Math.min(await candidates.count(), 20);
    for (let index = 0; index < count; index += 1) {
      const candidate = candidates.nth(index);
      if (!await candidate.isVisible().catch(() => false)) continue;
      const inDialog = await candidate.evaluate((node) => Boolean(node.closest(
        '[role="dialog"],[class*="modal" i]',
      ))).catch(() => false);
      if (!inDialog) continue;
      if (await candidate.click({ timeout: 1500 }).then(() => true).catch(() => false)) return name;
    }
  }
  return '';
}

async function clickEntryNamedEventually(page, names, timeoutMs = 10000) {
  const deadline = Date.now() + timeoutMs;
  do {
    const exact = await clickNamed(page, names);
    if (exact) return exact;
    for (const name of names || []) {
      const matches = page.getByText(name, { exact: false });
      const candidates = [];
      const count = await matches.count();
      for (let index = 0; index < count; index += 1) {
        const locator = matches.nth(index);
        if (!await locator.isVisible().catch(() => false)) continue;
        const text = String(await locator.textContent().catch(() => '')).replace(/\s+/g, ' ').trim();
        if (text.startsWith(name)) candidates.push({ locator, text });
      }
      candidates.sort((left, right) => left.text.length - right.text.length);
      if (candidates.length) {
        await candidates[0].locator.click();
        return name;
      }
    }
    await page.waitForTimeout(250);
  } while (Date.now() < deadline);
  return '';
}

async function inspectPage(page, rule = null) {
  const reviewed = {
    body: rule?.body || [],
    title: rule?.title || [],
    titleActivationSelectors: rule?.titleActivationSelectors || [],
    titleActivationNames: rule?.titleActivationNames || [],
    submitNames: rule?.submitNames || [],
    topicNames: rule?.topicControl?.triggerNames || [],
    imageUploadNames: rule?.mediaUploadTriggerNames?.image || [],
    videoUploadNames: rule?.mediaUploadTriggerNames?.video || [],
    scheduleNames: rule?.scheduleControl?.toggleNames || [],
    coverUploadNames: rule?.coverControl?.uploadTriggerNames || [],
    coverUploadInputSelectors: rule?.coverControl?.uploadInputSelectors || [],
    coverPreviewCanvasSelectors: rule?.coverControl?.previewCanvasSelectors || [],
	uploadRootSelector: rule?.uploadRootSelector || '',
	uploadPreviewSourcePattern: rule?.uploadPreviewSourcePattern || '',
    settingControls: Object.fromEntries(Object.entries(rule?.settingControls || {}).map(([key, control]) => [key, {
      selectors: control.selectors || [],
      names: control.names || control.triggerNames || [],
    }])),
  };
  return page.evaluate((reviewed) => {
    const visible = (node) => {
      const rect = node.getBoundingClientRect();
      const style = getComputedStyle(node);
      return rect.width > 0 && rect.height > 0 && style.visibility !== 'hidden' && style.display !== 'none';
    };
    const selectorVisible = (selectors) => (selectors || []).some((selector) => {
      try { return [...document.querySelectorAll(selector)].some(visible); } catch (_) { return false; }
    });
    const exactVisibleText = (names) => (names || []).some((name) => [...document.querySelectorAll('body *')]
      .some((node) => visible(node) && String(node.textContent || '').trim().replace(/\s+/g, ' ') === name));
    // Keep diagnostics useful without dumping a creator page or its draft
    // body. These are exact, reviewed setting labels only; new platform copy
    // must be added deliberately after a real-page observation.
    const settingLabelPattern = /^(?:作品声明|内容为 ?AI生成|允许下载(?:此作品)?|允许别人跟我拍同框|作品展示在同城页|允许评论|所有人可见|好友可见|仅自己可见|立即发布|定时发布|更多设置|发布设置|关联小程序|添加位置|选择音乐|添加合集|原创|转载|翻译|同时备份到 GitCode|全部可见|粉丝可见|VIP ?可见|发布文章)$/;
    const settingLabels = [...document.querySelectorAll('body *')]
      .filter(visible)
      .map((node) => String(node.innerText || '').trim().replace(/\s+/g, ' '))
      .filter((text) => settingLabelPattern.test(text))
      .filter((text, index, values) => values.indexOf(text) === index)
      .slice(0, 40);
    const fileInputs = [...document.querySelectorAll("input[type='file']")].slice(0, 20).map((node) => ({
      accept: node.getAttribute('accept') || '',
      multiple: node.hasAttribute('multiple'),
      class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
      visible: visible(node),
    }));
    const coverEditor = (() => {
      let inputs = [];
      let canvases = [];
      try {
        inputs = reviewed.coverUploadInputSelectors.flatMap((selector) => [...document.querySelectorAll(selector)]);
        canvases = reviewed.coverPreviewCanvasSelectors.flatMap((selector) => [...document.querySelectorAll(selector)]);
      } catch (_) {
        return { uploadTriggerVisible: false, inputCount: 0, inputs: [], canvasCount: 0, canvases: [] };
      }
      const signature = (canvas) => {
        if (!(canvas instanceof HTMLCanvasElement)) return '';
        try {
          const data = canvas.toDataURL('image/jpeg', 0.1);
          let hash = 0;
          for (let index = 0; index < data.length; index += 1) {
            hash = ((hash * 31) + data.charCodeAt(index)) | 0;
          }
          return `${canvas.width}x${canvas.height}:${data.length}:${hash}`;
        } catch (_) {
          return '';
        }
      };
      return {
        uploadTriggerVisible: exactVisibleText(reviewed.coverUploadNames),
        inputCount: inputs.length,
        inputs: inputs.slice(0, 4).map((node) => ({
          disabled: Boolean(node.disabled),
          accept: String(node.getAttribute('accept') || '').slice(0, 120),
          files: [...(node.files || [])].slice(0, 2).map((file) => ({
            name: file.name,
            size: file.size,
          })),
        })),
        canvasCount: canvases.length,
        canvases: canvases.slice(0, 4).map((node) => ({
          visible: visible(node),
          signature: signature(node),
        })),
      };
    })();
	const uploadPreview = (() => {
		if (!reviewed.uploadRootSelector) return { scopeVisible: false, candidateCount: 0, matchingSourceCount: 0, hosts: [] };
		let roots = [];
		try { roots = [...document.querySelectorAll(reviewed.uploadRootSelector)].filter(visible); } catch (_) { return { scopeVisible: false, candidateCount: 0, matchingSourceCount: 0, hosts: [] }; }
		const sources = roots.flatMap((root) => [...root.querySelectorAll('img,video,canvas')])
			.filter((node) => visible(node))
			.map((node) => node.currentSrc || node.src || node.poster || '')
			.filter(Boolean);
		let pattern = null;
		try { pattern = reviewed.uploadPreviewSourcePattern ? new RegExp(reviewed.uploadPreviewSourcePattern) : null; } catch (_) {}
		const hosts = [...new Set(sources.map((source) => {
			try { return new URL(source).hostname.toLowerCase(); } catch (_) { return ''; }
		}).filter(Boolean))].slice(0, 5);
		return {
			scopeVisible: roots.length === 1,
			candidateCount: Math.min(sources.length, 9),
			matchingSourceCount: Math.min(sources.filter((source) => !pattern || pattern.test(source)).length, 9),
			hosts,
		};
	})();
    // This is a bounded structural aid for selector maintenance. It never
    // returns element text, values, or page HTML, so a draft title/body cannot
    // leak through the diagnostic endpoint.
    const titleCandidates = [...document.querySelectorAll('input,textarea,[contenteditable="true"]')]
      .filter(visible)
      .filter((node) => /title|标题/i.test([
        node.id,
        node.className,
        node.getAttribute('placeholder'),
        node.getAttribute('aria-label'),
        node.getAttribute('data-placeholder'),
      ].filter(Boolean).join(' ')))
      .slice(0, 12)
      .map((node) => ({
        tag: node.tagName.toLowerCase(),
        id: String(node.id || '').slice(0, 120),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        placeholder: String(node.getAttribute('placeholder') || '').slice(0, 120),
        ariaLabel: String(node.getAttribute('aria-label') || '').slice(0, 120),
        readOnly: Boolean(node.readOnly),
        disabled: Boolean(node.disabled),
        contentEditable: node.getAttribute('contenteditable') === 'true',
      }));
    // When a platform's title selector drifts, expose only bounded structural
    // metadata for visible form fields. Values and text remain excluded.
    const formFieldCandidates = [...document.querySelectorAll('input,textarea,[contenteditable="true"],[role="combobox"],[role="checkbox"],[role="radio"]')]
      .filter(visible)
      .slice(0, 20)
      .map((node) => ({
        tag: node.tagName.toLowerCase(),
        role: String(node.getAttribute('role') || '').slice(0, 40),
        id: String(node.id || '').slice(0, 120),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        parentClass: String(node.parentElement?.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        grandparentClass: String(node.parentElement?.parentElement?.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        ancestorClasses: [...Array(5)].map((_, depth) => {
          let ancestor = node;
          for (let index = 0; index <= depth; index += 1) ancestor = ancestor?.parentElement;
          return String(ancestor?.className || '').trim().replace(/\s+/g, ' ').slice(0, 160);
        }),
        placeholder: String(node.getAttribute('placeholder') || '').slice(0, 120),
        ariaLabel: String(node.getAttribute('aria-label') || '').slice(0, 120),
        readOnly: Boolean(node.readOnly),
        disabled: Boolean(node.disabled),
        contentEditable: node.getAttribute('contenteditable') === 'true',
      }));
    const youtubeAdvancedControls = [...document.querySelectorAll(
      'ytcp-video-metadata-editor-advanced [role="checkbox"],'
      + 'ytcp-video-metadata-editor-advanced [role="radio"],'
      + 'ytcp-video-metadata-editor-advanced ytcp-dropdown-trigger',
    )]
      .filter(visible)
      .slice(0, 30)
      .map((node) => ({
        tag: node.tagName.toLowerCase(),
        role: String(node.getAttribute('role') || '').slice(0, 40),
        id: String(node.id || '').slice(0, 120),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        parentId: String(node.parentElement?.id || '').slice(0, 120),
        parentClass: String(node.parentElement?.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        grandparentId: String(node.parentElement?.parentElement?.id || '').slice(0, 120),
        grandparentClass: String(node.parentElement?.parentElement?.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        ariaLabel: String(node.getAttribute('aria-label') || '').slice(0, 120),
        ariaChecked: String(node.getAttribute('aria-checked') || '').slice(0, 10),
        selectedText: node.tagName.toLowerCase() === 'ytcp-dropdown-trigger'
          ? String(node.innerText || '').trim().replace(/\s+/g, ' ').slice(0, 120)
          : '',
      }));
    const youtubeImageCandidates = [...document.querySelectorAll(
      'ytcp-uploads-dialog img,ytcp-video-metadata-editor img',
    )]
      .slice(0, 30)
      .map((node) => ({
        tag: node.tagName.toLowerCase(),
        id: String(node.id || '').slice(0, 120),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        parentTag: node.parentElement?.tagName.toLowerCase() || '',
        parentId: String(node.parentElement?.id || '').slice(0, 120),
        parentClass: String(node.parentElement?.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        visible: visible(node),
        hasSource: Boolean(node.currentSrc || node.getAttribute('src')),
      }));
    const youtubeThumbnailStructure = [...document.querySelectorAll(
      'ytcp-video-metadata-editor-thumb,ytcp-video-metadata-editor-thumb *',
    )]
      .slice(0, 40)
      .map((node) => ({
        tag: node.tagName.toLowerCase(),
        id: String(node.id || '').slice(0, 120),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        role: String(node.getAttribute('role') || '').slice(0, 40),
        ariaLabel: String(node.getAttribute('aria-label') || '').slice(0, 120),
        visible: visible(node),
        hasBackground: getComputedStyle(node).backgroundImage !== 'none',
      }));
    const youtubeScheduleStructure = [...document.querySelectorAll(
      'ytcp-datetime-picker ytcp-text-dropdown-trigger,ytcp-datetime-picker input',
    )]
      .slice(0, 12)
      .map((node) => ({
        tag: node.tagName.toLowerCase(),
        id: String(node.id || '').slice(0, 120),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        visible: visible(node),
        type: node instanceof HTMLInputElement ? String(node.type || '').slice(0, 40) : '',
        inputMode: String(node.getAttribute('inputmode') || '').slice(0, 40),
        pattern: String(node.getAttribute('pattern') || '').slice(0, 80),
        maxLength: node instanceof HTMLInputElement ? node.maxLength : -1,
        selectionStart: node instanceof HTMLInputElement ? node.selectionStart : null,
        selectionEnd: node instanceof HTMLInputElement ? node.selectionEnd : null,
        active: document.activeElement === node,
        text: node.tagName.toLowerCase() === 'ytcp-text-dropdown-trigger'
          ? String(node.innerText || '').trim().replace(/\s+/g, ' ').slice(0, 80)
          : '',
        value: node instanceof HTMLInputElement ? String(node.value || '').slice(0, 40) : '',
      }));
    const youtubeTimePickerStructure = [...document.querySelectorAll(
      'tp-yt-iron-dropdown,'
      + 'tp-yt-paper-listbox,'
      + '[role="listbox"],'
      + '[role="option"],'
      + '[class*="time-of-day"],'
      + '[class*="time-picker"]',
    )]
      .filter(visible)
      .slice(0, 40)
      .map((node) => {
        const rawText = String(node.innerText || '').trim().replace(/\s+/g, ' ');
        return {
          tag: node.tagName.toLowerCase(),
          id: String(node.id || '').slice(0, 120),
          class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
          role: String(node.getAttribute('role') || '').slice(0, 40),
          text: /^(?:\d{1,2}(?::\d{2})?\s*(?:AM|PM)?|AM|PM)$/i.test(rawText)
            ? rawText.slice(0, 40)
            : '',
        };
      });
    const composerReadiness = {
      body: selectorVisible(reviewed.body),
      title: selectorVisible(reviewed.title),
      titleActivation: selectorVisible(reviewed.titleActivationSelectors) || exactVisibleText(reviewed.titleActivationNames),
      submit: exactVisibleText(reviewed.submitNames),
      topic: exactVisibleText(reviewed.topicNames),
      imageUploadTrigger: exactVisibleText(reviewed.imageUploadNames),
      videoUploadTrigger: exactVisibleText(reviewed.videoUploadNames),
      schedule: exactVisibleText(reviewed.scheduleNames),
      compatibleFileInput: fileInputs.some((item) => item.visible && (!item.accept || /image|video/i.test(item.accept))),
      settingControls: Object.fromEntries(Object.entries(reviewed.settingControls || {}).map(([key, control]) => [
        key,
        selectorVisible(control.selectors) || exactVisibleText(control.names),
      ])),
    };
    const pickerCandidates = [...document.querySelectorAll('body *')]
      .filter(visible)
      .filter((node) => /picker|calendar|time|date|popper|popover|dropdown|option/i.test([
        node.className,
        node.getAttribute('role'),
      ].filter(Boolean).join(' ')))
      .slice(0, 40)
      .map((node) => ({
        tag: node.tagName.toLowerCase(),
        role: String(node.getAttribute('role') || '').slice(0, 40),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 180),
        parentClass: String(node.parentElement?.className || '').trim().replace(/\s+/g, ' ').slice(0, 180),
      }));
    const scheduledPickerStructure = [...document.querySelectorAll('.scheduled-picker *')]
      .filter(visible)
      .slice(0, 60)
      .map((node) => ({
        tag: node.tagName.toLowerCase(),
        role: String(node.getAttribute('role') || '').slice(0, 40),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 180),
        parentClass: String(node.parentElement?.className || '').trim().replace(/\s+/g, ' ').slice(0, 180),
        childCount: node.children.length,
      }));
    const overlayStructure = [...document.querySelectorAll('body *')]
      .filter(visible)
      .filter((node) => {
        const style = getComputedStyle(node);
        return ['fixed', 'absolute'].includes(style.position) && Number(style.zIndex || 0) > 0;
      })
      .slice(0, 60)
      .map((node) => ({
        tag: node.tagName.toLowerCase(),
        role: String(node.getAttribute('role') || '').slice(0, 40),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 180),
        parentClass: String(node.parentElement?.className || '').trim().replace(/\s+/g, ' ').slice(0, 180),
        childCount: node.children.length,
      }));
    const calendarStructure = [...document.querySelectorAll('.calendar-wrapper *')]
      .filter(visible)
      .slice(0, 80)
      .map((node) => ({
        tag: node.tagName.toLowerCase(),
        role: String(node.getAttribute('role') || '').slice(0, 40),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 180),
        parentClass: String(node.parentElement?.className || '').trim().replace(/\s+/g, ' ').slice(0, 180),
        ariaLabel: String(node.getAttribute('aria-label') || '').slice(0, 100),
        title: String(node.getAttribute('title') || '').slice(0, 100),
        childCount: node.children.length,
      }));
    const numericPickerStructure = [...document.querySelectorAll('body *')]
      .filter(visible)
      .filter((node) => /^(?:[01]\d|2[0-3]|[0-5]\d)$/.test(String(node.textContent || '').trim()))
      .slice(0, 60)
      .map((node) => ({
        tag: node.tagName.toLowerCase(),
        role: String(node.getAttribute('role') || '').slice(0, 40),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 180),
        parentClass: String(node.parentElement?.className || '').trim().replace(/\s+/g, ' ').slice(0, 180),
      }));
    // Return only bounded error categories, not platform copy or draft text.
    const validationText = [...document.querySelectorAll('.el-form-item__error,.error-message,.form-error,.error-tip')]
      .filter(visible)
      .map((node) => String(node.innerText || node.textContent || '').replace(/\s+/g, ' ').trim())
      .join(' ');
    const validationErrors = {
      present: Boolean(validationText),
      title: /标题|title/i.test(validationText),
      body: /正文|内容|body|content/i.test(validationText),
      tags: /标签|tag/i.test(validationText),
      cover: /封面|cover/i.test(validationText),
      schedule: /定时|时间|schedule|time/i.test(validationText),
      visibility: /可见|公开|visibility/i.test(validationText),
    };
    const reviewedLabels = new Set(['图片', '发表', '发布', '发布笔记', '上传视频', '上传图文', '写长文', '拖拽或选择封面', '确定', '完成', '从正文选择', '从图片库选择']);
    const reviewedControls = [...document.querySelectorAll('body *')]
      .filter((node) => reviewedLabels.has((node.textContent || '').trim()) && visible(node))
      .slice(0, 30)
      .map((node) => {
        const ancestors = [];
        let current = node;
        for (let depth = 0; current && depth < 4; depth += 1, current = current.parentElement) {
          ancestors.push({
            tag: current.tagName.toLowerCase(),
            class: String(current.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
            role: current.getAttribute('role') || '',
            testID: current.getAttribute('data-testid') || '',
            href: current.tagName === 'A' ? (() => {
              try {
                const href = new URL(current.href);
                return `${href.pathname}${href.search}`;
              } catch (_) {
                return '';
              }
            })() : '',
          });
        }
        return { label: (node.textContent || '').trim(), ancestors };
      });
    const dialog = [...document.querySelectorAll('[role="dialog"],.weui-desktop-dialog,.modal__inner-2,.publish-article-modal__body')]
      .find(visible);
    const dialogImages = dialog
      ? [...dialog.querySelectorAll('img')].filter(visible).slice(0, 20).map((node) => ({
        alt: node.getAttribute('alt') || '',
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        width: Math.round(node.getBoundingClientRect().width),
        height: Math.round(node.getBoundingClientRect().height),
        parentTag: node.parentElement?.tagName.toLowerCase() || '',
        parentClass: String(node.parentElement?.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        grandparentClass: String(node.parentElement?.parentElement?.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
      }))
      : [];
    const dialogImageTiles = dialog
      ? [...dialog.querySelectorAll('div,li,a')].filter((node) => {
        if (!visible(node)) return false;
        const rect = node.getBoundingClientRect();
        const background = getComputedStyle(node).backgroundImage;
        return rect.width >= 60 && rect.height >= 60 && background && background !== 'none';
      }).slice(0, 20).map((node) => ({
        tag: node.tagName.toLowerCase(),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        width: Math.round(node.getBoundingClientRect().width),
        height: Math.round(node.getBoundingClientRect().height),
        parentClass: String(node.parentElement?.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
      }))
      : [];
    const dialogElements = dialog
      ? [...dialog.querySelectorAll('*')].filter((node) => {
        if (!visible(node)) return false;
        const rect = node.getBoundingClientRect();
        return rect.width >= 40 && rect.height >= 40;
      }).slice(0, 60).map((node) => ({
        tag: node.tagName.toLowerCase(),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        role: node.getAttribute('role') || '',
        width: Math.round(node.getBoundingClientRect().width),
        height: Math.round(node.getBoundingClientRect().height),
      }))
      : [];
    // A bounded allow-list is enough to diagnose native media editors without
    // exposing draft text. It identifies only platform action labels and the
    // control that owns them.
    const dialogActionLabels = [...document.querySelectorAll('body *')]
      .filter(visible)
      .filter((node) => ['确认上传', '取消'].includes(String(node.innerText || '').trim()))
      .slice(0, 8)
      .map((node) => ({
        label: String(node.innerText || '').trim(),
        tag: node.tagName.toLowerCase(),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        role: node.getAttribute('role') || '',
        href: node.tagName === 'A' ? String(node.getAttribute('href') || '').slice(0, 80) : '',
        width: Math.round(node.getBoundingClientRect().width),
        height: Math.round(node.getBoundingClientRect().height),
      }));
    // Diagnostics for a blocking modal may expose only close semantics and
    // structure, never arbitrary text, values, or markup from the draft.
    const overlayCloseCandidates = [...document.querySelectorAll('body *')]
      .filter(visible)
      .map((node) => {
        const marker = [node.getAttribute('aria-label') || '', node.getAttribute('title') || '', node.textContent || '', node.className || '']
          .join(' ').trim().replace(/\s+/g, ' ');
        return { node, marker };
      })
      .filter(({ marker }) => /(^|\s)(×|x)(\s|$)|关闭|close/i.test(marker))
      .slice(0, 30)
      .map(({ node, marker }) => ({
        tag: node.tagName.toLowerCase(),
        class: String(node.className || '').trim().replace(/\s+/g, ' ').slice(0, 160),
        role: node.getAttribute('role') || '',
        ariaLabel: String(node.getAttribute('aria-label') || '').slice(0, 80),
        title: String(node.getAttribute('title') || '').slice(0, 80),
        marker: marker.slice(0, 24),
        width: Math.round(node.getBoundingClientRect().width),
        height: Math.round(node.getBoundingClientRect().height),
      }));
    return { title: document.title.slice(0, 120), composerReadiness, validationErrors, settingLabels, fileInputs, coverEditor, uploadPreview, titleCandidates, formFieldCandidates, youtubeAdvancedControls, youtubeImageCandidates, youtubeThumbnailStructure, youtubeScheduleStructure, youtubeTimePickerStructure, pickerCandidates, scheduledPickerStructure, overlayStructure, calendarStructure, numericPickerStructure, reviewedControls, dialogImages, dialogImageTiles, dialogElements, dialogActionLabels, overlayCloseCandidates };
  }, reviewed);
}

// This is deliberately narrower than prepare: it only exposes the current
// platform's reviewed options surface. It never writes a value, uploads media,
// or reaches a terminal publish control.
async function revealReviewSettings(page, platform) {
  if (platform === 'tiktok' || platform === 'youtube') {
    const match = await uniqueVisibleExactText(page, ['Show more', '显示更多']);
    if (match.ambiguous || !match.control) return '';
    if (!await match.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) return '';
    await page.waitForTimeout(200);
    return '显示更多';
  }
  if (platform === 'kuaishou') {
    const match = await uniqueVisibleExactText(page, ['发布设置']);
    if (match.ambiguous || !match.control) return '';
    if (!await match.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) return '';
    await page.waitForTimeout(200);
    return '发布设置';
  }
  if (platform === 'csdn') {
    const advance = page.locator('button.btn.btn-publish:not(.btn-b-red)');
    if (await advance.count() !== 1 || !await advance.isVisible().catch(() => false)) return '';
    if (!await advance.click({ timeout: 1500 }).then(() => true).catch(() => false)) return '';
    // The first-stage click is not itself evidence that the options modal
    // opened. Wait for CSDN's reviewed modal body so a slow or ignored click
    // cannot be reported as a successful settings reveal.
    return await firstVisibleEventually(page, ['.publish-article-modal__body', '.modal__inner-2'], 3000)
      ? '发布选项'
      : '';
  }
  return '';
}

async function fillLocator(locator, value, options = {}) {
  if (!locator || !value) return false;
  try {
    const tag = await locator.evaluate((node) => node.tagName.toLowerCase());
    if (tag === 'iframe') {
      const frame = locator.contentFrame();
      const editor = frame.locator("body[contenteditable='true'],div[contenteditable='true']").first();
      if (!await editor.count()) return false;
      await editor.fill(value, { timeout: 1500 });
      return await verifyOrRefillContentEditable(editor, value, options);
    }
    if (options.nativeTyping && tag !== 'input' && tag !== 'textarea') {
      await locator.click({ timeout: 1500 });
      await locator.press(process.platform === 'darwin' ? 'Meta+A' : 'Control+A');
      await locator.type(value, { delay: 20 });
      return await verifyOrRefillContentEditable(locator, value, options);
    }
    await locator.fill(value, { timeout: 1500 });
    if (tag === 'input' || tag === 'textarea') return await locator.inputValue() === value;
    return await verifyOrRefillContentEditable(locator, value, options);
  } catch (_) {
    return false;
  }
}

async function verifyOrRefillContentEditable(locator, value, options) {
  const matches = async () => {
    const rendered = await locator.evaluate((node) => ({ innerText: node.innerText, textContent: node.textContent }));
    if (contentEditableValueMatches(rendered.innerText, value) || contentEditableValueMatches(rendered.textContent, value)) return true;
    if (!options.whitespaceEquivalent) return false;
    const compact = (text) => String(text || '').replace(/\s+/gu, '');
    return compact(rendered.innerText) === compact(value) || compact(rendered.textContent) === compact(value);
  };
  if (await matches()) return true;
  // ProseMirror-based creator editors can reject Playwright's direct fill
  // while still exposing a real contenteditable node. Use the browser's native
  // editing command once, then verify the rendered value again.
  await locator.evaluate((node, text) => {
    node.focus();
    if (node.matches('pre.editor__inner')) {
      node.textContent = text;
      node.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: text }));
      return;
    }
    const selection = window.getSelection();
    const range = document.createRange();
    range.selectNodeContents(node);
    selection.removeAllRanges();
    selection.addRange(range);
    if (!document.execCommand('insertText', false, text)) {
      node.textContent = text;
      node.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: text }));
    }
  }, value);
  await new Promise((resolve) => setTimeout(resolve, 100));
  return await matches();
}

async function contentEditableDiagnostics(locator, expected) {
  if (!locator) return {};
  try {
    const rendered = await locator.evaluate((node) => ({ innerText: node.innerText || '', textContent: node.textContent || '' }));
    const compact = (text) => String(text).replace(/\s+/gu, '');
    return {
      expectedLength: expected.length,
      innerTextLength: rendered.innerText.length,
      textContentLength: rendered.textContent.length,
      expectedNewlines: (expected.match(/\n/g) || []).length,
      innerTextNewlines: (rendered.innerText.match(/\n/g) || []).length,
      textContentNewlines: (rendered.textContent.match(/\n/g) || []).length,
      innerTextWhitespaceEquivalent: compact(rendered.innerText) === compact(expected),
      textContentWhitespaceEquivalent: compact(rendered.textContent) === compact(expected),
    };
  } catch (_) {
    return {};
  }
}

function contentEditableValueMatches(actual, expected) {
  actual = String(actual || '').replace(/\r\n?/g, '\n');
  expected = String(expected || '').replace(/\r\n?/g, '\n');
  if (actual === expected) return true;
  // Douyin's reviewed editor replaces only the blank-line-before-hashtag
  // boundary with zero-width separators. Keep this exact transform scoped so
  // a changed body or general whitespace loss still fails closed.
  if (actual.includes('\u200b') && expected.includes('\n\n#')
    && actual.replace(/\u200b/g, '') === expected.replace(/\n\n(?=#)/g, '')) return true;
  const normalizeEditorSpace = (text) => text
    .replace(/[\u200b\uFEFF]/g, '')
    .replace(/\u00a0/g, ' ')
    .replace(/[ \t]+$/gm, '');
  actual = normalizeEditorSpace(actual);
  expected = normalizeEditorSpace(expected);
  if (actual === expected) return true;
  // CSDN's cledit editor materializes one terminal LF after a successful
  // contenteditable fill. Accept only that known editor artifact, never a
  // changed body or arbitrary whitespace loss.
  return !expected.endsWith('\n') && actual === `${expected}\n`;
}

function contentBodyWithTags(content) {
  return content.body || content.caption;
}

function bodyWithoutFrozenTrailingTags(content, rule) {
  const value = contentBodyWithTags(content);
  if (!rule.topicControl?.exactNativeTopicSet && !rule.topicControl?.stripFrozenTrailingTags) return value;
  const expected = [...new Set(
    content.tags.map((tag) => tag.trim().replace(/^#+/, '').trim()).filter(Boolean),
  )];
  if (!expected.length) return value;
  const trimmedEnd = value.replace(/[ \t]*(?:\r?\n[ \t]*)*$/u, '');
  const lineStart = Math.max(trimmedEnd.lastIndexOf('\n'), trimmedEnd.lastIndexOf('\r')) + 1;
  const line = trimmedEnd.slice(lineStart).trim();
  if (!/^#[^\s#]+(?:[ \t]+#[^\s#]+)*$/u.test(line)) return value;
  const actual = line.split(/[ \t]+/u).map((tag) => tag.slice(1));
  if (actual.length !== expected.length || new Set(actual).size !== actual.length
    || expected.some((tag) => !actual.includes(tag))) return value;
  return trimmedEnd.slice(0, lineStart).replace(/(?:\r?\n)+$/u, '');
}

function keyboardFor(scope) {
  return scope.keyboard || scope.page().keyboard;
}

function hasTrailingTopicMarker(before, after) {
  return after !== before && /#[ \t\u00a0\u200b\uFEFF]*$/u.test(after);
}

function trailingTopicPlaceholderLength(value) {
  const match = value.match(/#([ \t\u00a0\u200b\uFEFF]+)$/u);
  return match ? [...match[1]].length : 0;
}

function topicMarkerDiagnostics(before, after) {
  const describe = (value) => ({
    tail: value.slice(-16),
    codepoints: [...value.slice(-16)].map((character) => character.codePointAt(0).toString(16)),
  });
  return { before: describe(before), after: describe(after) };
}

async function topicSelectionDiagnostics(locator) {
  return locator.evaluate((root) => {
    const selection = window.getSelection();
    return {
      editorActive: root === document.activeElement,
      anchorInEditor: Boolean(selection?.anchorNode && root.contains(selection.anchorNode)),
      anchorOffset: selection?.anchorOffset ?? -1,
      anchorText: (selection?.anchorNode?.textContent || '').slice(-40),
      htmlTail: (root.innerHTML || '').slice(-500),
    };
  }).catch(() => ({}));
}

async function isSelectedTopic(locator, tag) {
  if (!locator) return false;
  const expected = `#${tag}`;
  return locator.evaluate((root, expectedText) => {
    const rootColor = getComputedStyle(root).color;
    return [...root.querySelectorAll('*')].some((node) => {
      const normalizedText = (node.textContent || '').replace(/[\s\u200b\uFEFF]+/g, '');
      let nativeTopicName = '';
      try {
        nativeTopicName = JSON.parse(node.getAttribute('data-topic') || '{}').name || '';
      } catch (_) {
        nativeTopicName = '';
      }
      if (normalizedText !== expectedText && nativeTopicName !== expectedText.slice(1)) return false;
      // TikTok Studio's reviewed DraftJS editor commits hashtags as an
      // official mention entity. Its current light theme keeps the same text
      // color, so the exact structural marker is the proof, not blue styling.
      if (node.matches('span.mention[data-testid="mentionText"]')) return true;
      const attributes = [...node.attributes].map((attribute) => `${attribute.name}=${attribute.value}`).join(' ').toLowerCase();
      const className = String(node.className || '').toLowerCase();
      const selectedMarker = attributes.includes('topic') || attributes.includes('mention')
        || /(^|[-_])(topic|mention|hashtag|tag)([-_]|$)/.test(className);
      const nonEditableToken = node.getAttribute('contenteditable') === 'false';
      const style = getComputedStyle(node);
      const background = style.backgroundColor;
      const border = style.borderColor;
      const highlighted = style.color !== rootColor
        || (background && background !== 'rgba(0, 0, 0, 0)' && background !== 'transparent')
        || (border && border !== 'rgba(0, 0, 0, 0)' && style.borderStyle !== 'none');
      const markerAncestor = node.closest('[data-mention],[data-topic],[data-topic-selected],[data-tag-selected]');
      const nativeMarker = selectedMarker || nonEditableToken || Boolean(markerAncestor)
        || node.matches('a,[role="link"],[role="option"]');
      // A native marker without the platform's highlighted selected state is
      // still ordinary text from the operator's perspective.
      return nativeMarker && highlighted;
    });
  }, expected).catch(() => false);
}

async function focusEditorEnd(locator) {
  await locator.focus();
  await locator.evaluate((node) => {
    node.focus();
    if (!node.isContentEditable) {
      if (typeof node.setSelectionRange === 'function') node.setSelectionRange(node.value.length, node.value.length);
      return;
    }
    const selection = window.getSelection();
    const range = document.createRange();
    range.selectNodeContents(node);
    range.collapse(false);
    selection.removeAllRanges();
    selection.addRange(range);
  });
}

async function clickExactTopicSuggestion(page, tag) {
  const expected = `#${tag}`;
  const officialItems = page.locator('#creator-editor-topic-container .item');
  const officialCount = Math.min(await officialItems.count(), 30);
  for (let index = 0; index < officialCount; index += 1) {
    const item = officialItems.nth(index);
    if (!await item.isVisible().catch(() => false)) continue;
    const name = (await item.locator('.name').first().innerText().catch(() => '')).trim();
    if (name !== expected) continue;
    if (await item.click({ timeout: 1500 }).then(() => true).catch(() => false)) return true;
    // Ponytail: the reviewed XHS topic parent is the actual Vue click target;
    // a child span can be visually present yet intercepted by the popup.
    if (await item.evaluate((node) => {
      node.click();
      return true;
    }).catch(() => false)) return true;
  }
  for (const label of [expected, tag]) {
    const exactMatches = page.getByText(label, { exact: true });
    const exactCount = Math.min(await exactMatches.count(), 20);
    for (let index = 0; index < exactCount; index += 1) {
      const candidate = exactMatches.nth(index);
      if (!await candidate.isVisible().catch(() => false)) continue;
      const isSuggestion = await candidate.evaluate((node) => {
        const editor = node.closest("[contenteditable='true'],textarea");
        if (node.closest('#creator-editor-topic-container .item')) return true;
        let current = node;
        for (let depth = 0; current && current !== document.body && current !== editor && depth < 7; depth += 1, current = current.parentElement) {
          const role = current.getAttribute('role') || '';
          const className = String(current.className || '').toLowerCase();
          if (role === 'option' || role === 'listbox' || current.matches('li')
            || /(topic|suggest|popover|dropdown|option)/.test(className)) return true;
        }
        return false;
      }).catch(() => false);
      if (!isSuggestion) continue;
      if (await candidate.click({ timeout: 1500 }).then(() => true).catch(() => false)) return true;
    }
  }
  const selectors = [
    "[role='option']",
    "[role='listbox'] li",
    "[class*='suggest' i] li",
    "[class*='topic' i] li",
    "[class*='popover' i] li",
    "[class*='dropdown' i] li",
    "[class*='suggest' i] [role='button']",
    "[class*='topic' i] [role='button']",
  ];
  for (const selector of selectors) {
    const candidates = page.locator(selector);
    const count = Math.min(await candidates.count(), 30);
    for (let index = 0; index < count; index += 1) {
      const candidate = candidates.nth(index);
      if (!await candidate.isVisible().catch(() => false)) continue;
      const text = (await candidate.innerText().catch(() => '')).trim();
      if (text !== expected && !text.startsWith(`${expected} `) && text !== tag && !text.startsWith(`${tag} `)) continue;
      if (await candidate.click({ timeout: 1500 }).then(() => true).catch(() => false)) return true;
    }
  }
  return false;
}

async function typeInlinePickerTopic(page, tag, definition, includeHash = true) {
  const keyboard = keyboardFor(page);
  if (includeHash) await keyboard.type('#', { delay: definition.typingDelayMs || 30 });
  let partial = '';
  for (const character of [...tag]) {
    await keyboard.type(character, { delay: definition.typingDelayMs || 30 });
    partial += character;
    const ready = await page.waitForFunction((expected) => [...document.querySelectorAll(
      '#creator-editor-topic-container .item .name',
    )].some((node) => (node.textContent || '').trim() === expected), `#${partial}`, {
      timeout: definition.characterSettleTimeoutMs || 1500,
    }).then(() => true).catch(() => false);
    if (!ready) return false;
  }
  return true;
}

async function warmInlineTopicProvider(page, locator, tag, definition) {
  const keyboard = keyboardFor(page);
  const firstCharacter = [...tag][0];
  if (!firstCharacter) return true;
  const baseline = await locator.locator('a.tiptap-topic[data-topic]').count();
  await keyboard.type(`#${firstCharacter}`, { delay: definition.typingDelayMs || 30 });
  await page.waitForTimeout(definition.characterSettleTimeoutMs || 1500);
  await keyboard.press('Space');
  await page.waitForTimeout(500);
  const warmed = await isSelectedTopic(locator, firstCharacter);
  for (let attempts = 0; attempts < 8; attempts += 1) {
    const nativeCount = await locator.locator('a.tiptap-topic[data-topic]').count();
    const text = await locator.evaluate((node) => node.innerText || node.value || '');
    if (nativeCount === baseline && !text.trimEnd().endsWith(`#${firstCharacter}`)) return warmed;
    await keyboard.press('Backspace');
    await page.waitForTimeout(100);
  }
  return false;
}

async function commitRecommendedTopics(page, locator, tags, definition) {
  const selected = [];
  const normalized = [...new Set(tags.map((tag) => tag.trim().replace(/^#+/, '').trim()).filter(Boolean))];
  if (!normalized.length) return { selected, missing: [], diagnostics: [] };
  if ((definition.triggerNames || []).length && !await clickNamed(page, definition.triggerNames)) {
    return { selected, missing: normalized, diagnostics: [{ recommendationPicker: 'trigger_missing' }] };
  }
  const desiredCount = Math.min(normalized.length, definition.maxTopics || normalized.length);
  const recommendationDeadline = Date.now() + (definition.settleMs || 1000);
  let fallbackReadyAt = 0;
  do {
    let exactReady = false;
    let visibleRecommendations = 0;
    for (const selector of definition.recommendationSelectors || []) {
      const candidates = page.locator(selector);
      const count = Math.min(await candidates.count(), 30);
      for (let index = 0; index < count; index += 1) {
        const candidate = candidates.nth(index);
        if (!await candidate.isVisible().catch(() => false)) continue;
        visibleRecommendations += 1;
        const actual = (await candidate.innerText().catch(() => '')).trim().replace(/^#+/, '').trim();
        if (normalized.slice(0, desiredCount).includes(actual)) {
          exactReady = true;
          break;
        }
      }
      if (exactReady) break;
    }
    if (definition.allowRecommendationFallback && visibleRecommendations >= desiredCount) {
      fallbackReadyAt ||= Date.now();
    } else {
      fallbackReadyAt = 0;
    }
    if (exactReady || (fallbackReadyAt && Date.now() - fallbackReadyAt >= 1000) || Date.now() >= recommendationDeadline) break;
    await page.waitForTimeout(250);
  } while (true);
  for (const tag of normalized.slice(0, desiredCount)) {
    let matched = false;
    for (const selector of definition.recommendationSelectors || []) {
      const candidates = page.locator(selector);
      const count = Math.min(await candidates.count(), 30);
      for (let index = 0; index < count; index += 1) {
        const candidate = candidates.nth(index);
        if (!await candidate.isVisible().catch(() => false)) continue;
        const actual = (await candidate.innerText().catch(() => '')).trim().replace(/^#+/, '').trim();
        if (actual !== tag) continue;
        if (!await candidate.click({ timeout: 1500 }).then(() => true).catch(() => false)) break;
        await page.waitForTimeout(250);
        if (await isSelectedTopic(locator, tag)) {
          selected.push(tag);
          matched = true;
        }
        break;
      }
      if (matched) break;
    }
  }
  if (definition.allowRecommendationFallback && selected.length < desiredCount) {
    for (const selector of definition.recommendationSelectors || []) {
      const candidates = page.locator(selector);
      const count = Math.min(await candidates.count(), 30);
      for (let index = 0; index < count; index += 1) {
        const candidate = candidates.nth(index);
        if (!await candidate.isVisible().catch(() => false)) continue;
        const text = (await candidate.innerText().catch(() => '')).trim();
        const match = text.match(/#([^\s#]+)/u);
        const fallbackTag = match?.[1] || '';
        if (!fallbackTag || selected.includes(fallbackTag)) continue;
        if (!await candidate.click({ timeout: 1500 }).then(() => true).catch(() => false)) continue;
        await page.waitForTimeout(250);
        if (await isSelectedTopic(locator, fallbackTag)) selected.push(fallbackTag);
        if (selected.length >= desiredCount) break;
      }
      if (selected.length >= desiredCount) break;
    }
  }
  const complete = selected.length >= desiredCount;
  return {
    selected,
    missing: complete ? [] : normalized.slice(selected.length, desiredCount),
    diagnostics: complete ? [] : [{ recommendationPicker: 'native_topic_missing' }],
  };
}

async function inspectTopicTokens(locator, tag) {
  if (!locator) return [];
  const expected = `#${tag}`;
  return locator.evaluate((root, expectedText) => [...root.querySelectorAll('*')]
    .filter((node) => (node.textContent || '').replace(/[\s\u200b\uFEFF]+/g, '').includes(expectedText))
    .slice(0, 12)
    .map((node) => ({
      tag: node.tagName.toLowerCase(),
      class: String(node.className || '').slice(0, 160),
      role: node.getAttribute('role') || '',
      contenteditable: node.getAttribute('contenteditable') || '',
      color: getComputedStyle(node).color,
      rootColor: getComputedStyle(root).color,
      background: getComputedStyle(node).backgroundColor,
      attributes: [...node.attributes].map((attribute) => `${attribute.name}=${attribute.value}`).join(' ').slice(0, 240),
    })), expected).catch(() => []);
}

async function inspectTopicSuggestions(page, tag) {
  const result = [];
  for (const label of [`#${tag}`, tag]) {
    const matches = page.getByText(label, { exact: false });
    const count = Math.min(await matches.count(), 30);
    for (let index = 0; index < count && result.length < 12; index += 1) {
      const match = matches.nth(index);
      if (!await match.isVisible().catch(() => false)) continue;
      const candidate = await match.evaluate((node) => {
        if (node.closest("[contenteditable='true'],textarea")) return null;
        return {
          tag: node.tagName.toLowerCase(),
          text: (node.innerText || node.textContent || '').trim().slice(0, 120),
          class: String(node.className || '').slice(0, 160),
          role: node.getAttribute('role') || '',
        };
      }).catch(() => null);
      if (candidate && candidate.text) result.push(candidate);
    }
  }
  return result;
}

async function nativeTopicSet(locator, definition) {
  if (!locator || !definition.nativeTopicSelectors?.length) return { topics: [], invalid: true };
  return locator.locator(definition.nativeTopicSelectors.join(',')).evaluateAll((nodes) => {
    const topics = [];
    let invalid = false;
    for (const node of nodes) {
      let name = '';
      try {
        name = JSON.parse(node.getAttribute('data-topic') || '{}').name || '';
      } catch (_) {
        invalid = true;
      }
      name = String(name).trim().replace(/^#+/, '').trim();
      if (!name) invalid = true;
      else topics.push(name);
    }
    return { topics, invalid };
  }).catch(() => ({ topics: [], invalid: true }));
}

function exactTopicSetMatches(actual, expected) {
  if (actual.length !== expected.length) return false;
  const counts = new Map();
  for (const topic of actual) counts.set(topic, (counts.get(topic) || 0) + 1);
  return expected.every((topic) => counts.get(topic) === 1);
}

function removableNativeTopicIndex(actual, expected) {
  const expectedSet = new Set(expected);
  const seen = new Map();
  for (let index = 0; index < actual.length; index += 1) {
    const topic = actual[index];
    if (!expectedSet.has(topic)) return index;
    seen.set(topic, (seen.get(topic) || 0) + 1);
    if (seen.get(topic) > 1) return index;
  }
  return -1;
}

async function reconcileNativeTopicSet(page, locator, expected, definition) {
  const removed = [];
  const limit = Math.max(0, Math.min(definition.maxNativeTopicRemovals || 0, 20));
  let native = await nativeTopicSet(locator, definition);
  for (let attempt = 0; attempt < limit && !native.invalid && !exactTopicSetMatches(native.topics, expected); attempt += 1) {
    if (expected.some((topic) => !native.topics.includes(topic))) break;
    const index = removableNativeTopicIndex(native.topics, expected);
    if (index < 0) break;
    const nodes = locator.locator(definition.nativeTopicSelectors.join(','));
    if (await nodes.count() !== native.topics.length) break;
    const selected = await nodes.nth(index).evaluate((node) => {
      const editor = node.closest("[contenteditable='true']");
      if (!editor) return false;
      editor.focus();
      const selection = window.getSelection();
      const range = document.createRange();
      range.selectNode(node);
      selection.removeAllRanges();
      selection.addRange(range);
      return selection.rangeCount === 1 && selection.containsNode(node, false);
    }).catch(() => false);
    if (!selected) break;
    await keyboardFor(page).press('Backspace');
    await page.waitForTimeout(100);
    const after = await nativeTopicSet(locator, definition);
    if (after.invalid || after.topics.length !== native.topics.length - 1) {
      native = after;
      break;
    }
    removed.push(native.topics[index]);
    native = after;
  }
  return { native, removed };
}

async function exactBodyWithoutNativeTopics(locator, definition, expectedBody) {
  if (!locator) return false;
  const actual = await locator.evaluate((root, selectors) => {
    if (root.matches('input,textarea')) return root.value;
    const copy = root.cloneNode(true);
    copy.querySelectorAll(selectors.join(',')).forEach((topic) => topic.remove());
    return copy.innerText || copy.textContent || '';
  }, definition.nativeTopicSelectors).catch(() => null);
  if (actual === null) return false;
  const trimTopicWhitespace = (value) => String(value)
    .replace(/[\u200b\uFEFF]/gu, '')
    .replace(/\u00a0/gu, ' ')
    .replace(/[ \t]+$/gmu, '')
    .trimEnd();
  const normalizedActual = trimTopicWhitespace(actual);
  const normalizedExpected = trimTopicWhitespace(expectedBody);
  if (contentEditableValueMatches(normalizedActual, normalizedExpected)) return true;
  if (!definition.bodyWhitespaceEquivalent) return false;
  const compact = (value) => value.replace(/\s+/gu, '');
  return compact(normalizedActual) === compact(normalizedExpected);
}

async function hasSelectedTagChip(page, tag) {
  const youtubeChips = page.locator('ytcp-chip[role="button"]');
  const youtubeChipCount = Math.min(await youtubeChips.count(), 20);
  for (let index = 0; index < youtubeChipCount; index += 1) {
    const chip = youtubeChips.nth(index);
    if (!await chip.isVisible().catch(() => false)) continue;
    const selected = await chip.evaluate((node, expected) => {
      const text = (node.textContent || '').trim();
      const ariaLabel = node.getAttribute('aria-label') || '';
      return text === expected && /press enter to edit/i.test(ariaLabel);
    }, tag).catch(() => false);
    if (selected) return true;
  }
  const matches = page.getByText(tag, { exact: true });
  const count = Math.min(await matches.count(), 20);
  for (let index = 0; index < count; index += 1) {
    const match = matches.nth(index);
    if (!await match.isVisible().catch(() => false)) continue;
    const selected = await match.evaluate((node) => {
      if (node.closest('input,textarea,[contenteditable="true"],[role="option"],[role="listbox"]')) return false;
      for (let current = node, depth = 0; current && current !== document.body && depth < 5; current = current.parentElement, depth += 1) {
        const attributes = [...current.attributes].map((attribute) => `${attribute.name}=${attribute.value}`).join(' ').toLowerCase();
        const className = String(current.className || '').toLowerCase();
        const marker = attributes.includes('selected') || attributes.includes('chip') || attributes.includes('token')
          || /(^|[-_])(tag|chip|token)([-_]|$)/.test(className);
        if (!marker) continue;
        const style = getComputedStyle(current);
        const background = style.backgroundColor;
        const border = style.borderColor;
        const highlighted = style.color !== getComputedStyle(current.parentElement || document.body).color
          || (background && background !== 'rgba(0, 0, 0, 0)' && background !== 'transparent')
          || (border && border !== 'rgba(0, 0, 0, 0)' && style.borderStyle !== 'none');
        const removable = Boolean(current.querySelector('button,[role="button"],svg,[aria-label*="删除"],[aria-label*="移除"]'));
        const editableYouTubeChip = current.tagName.toLowerCase() === 'ytcp-chip'
          && current.getAttribute('role') === 'button'
          && /press enter to edit/i.test(current.getAttribute('aria-label') || '');
        const explicit = current.getAttribute('aria-selected') === 'true'
          || current.getAttribute('data-selected') === 'true'
          || current.getAttribute('data-tag-selected') === 'true';
        return highlighted || removable || editableYouTubeChip || explicit;
      }
      return false;
    }).catch(() => false);
    if (selected) return true;
  }
  return false;
}

async function commitPlatformTopics(page, locator, tags, definition, expectedBody = '') {
  const keyboard = keyboardFor(page);
  const selected = [];
  const missing = [];
  const diagnostics = [];
  const normalized = [...new Set(tags.map((tag) => tag.trim().replace(/^#+/, '').trim()).filter(Boolean))];
  if (definition.preferCJK) {
    normalized.sort((left, right) => Number(!/[\u3400-\u9fff]/u.test(left)) - Number(!/[\u3400-\u9fff]/u.test(right)));
  }
  if (definition.maxTopics) normalized.splice(definition.maxTopics);
  if (!normalized.length) return { selected, missing, diagnostics };
  if (!locator) return { selected, missing: normalized, diagnostics };
  if (definition.mode === 'kuaishou_adaptive') {
    const recommendationResult = await commitRecommendedTopics(page, locator, normalized, definition);
    if (recommendationResult.selected.length > 0) {
      return recommendationResult;
    }
    const inlineTrigger = await firstVisibleNamedControl(page, definition.inlineTriggerNames || [], ['button']);
    // Ponytail: Kuaishou graph-text currently exposes a native “#话题”
    // trigger, while its video editor may expose only the AI recommendation
    // panel. Prefer the exact native trigger when present; otherwise type the
    // requested topic and still require a selected token after Space.
    definition = inlineTrigger
      ? {
        ...definition,
        mode: 'trigger_picker',
        triggerNames: definition.inlineTriggerNames,
        commitKey: 'Space',
        preserveNativeFocus: true,
        settleMs: 1500,
      }
      : {
        ...definition,
        mode: 'plain_space',
        commitKey: 'Space',
        settleMs: 1500,
      };
  }
  if (definition.mode === 'recommendation_picker') {
    return commitRecommendedTopics(page, locator, normalized, definition);
  }
  const rollbackUncommittedTopic = async (tag) => {
    const current = await locator.evaluate((node) => node.innerText || node.value || '');
    const trimmed = current.trimEnd();
    const suffix = `#${tag}`;
    if (!trimmed.endsWith(suffix)) return;
    await keyboard.press('Escape').catch(() => {});
    await focusEditorEnd(locator);
    const removeCount = [...current.slice(trimmed.length - suffix.length)].length;
    for (let index = 0; index < removeCount; index += 1) await keyboard.press('Backspace');
  };
  try {
    const existing = await locator.evaluate((node) => (node.innerText || node.value || '').trim());
    if (existing) {
      await focusEditorEnd(locator);
      await locator.press('Enter');
      await locator.press('Enter');
    }
    if (definition.readyDelayMs) await page.waitForTimeout(definition.readyDelayMs);
    let activatedTopicFocus = false;
    if (definition.activationTriggerNames) {
      if (!await clickNamed(page, definition.activationTriggerNames)) {
        return { selected, missing: normalized, diagnostics };
      }
      await page.waitForTimeout(definition.activationSettleMs || 300);
      // Activate the platform's topic provider, then remove only its temporary
      // marker. The actual tag is typed below and must still resolve to an
      // exact native candidate.
      await keyboard.press('Backspace');
      await page.waitForTimeout(250);
      activatedTopicFocus = true;
    }
    if (definition.warmupFirstCharacter) {
      const firstTag = normalized[0];
      if (!await warmInlineTopicProvider(page, locator, firstTag, definition)) {
        return { selected, missing: normalized, diagnostics: [{ tag: firstTag, warmup: 'failed' }] };
      }
      await page.waitForTimeout(1000);
      activatedTopicFocus = false;
    }
    for (const tag of normalized) {
      if (selected.length && definition.betweenTopicsSettleMs) {
        await page.waitForTimeout(definition.betweenTopicsSettleMs);
      }
      let markerDiagnostics = {};
      if (!activatedTopicFocus) await focusEditorEnd(locator);
      activatedTopicFocus = false;
      if (definition.mode === 'trigger_picker') {
        const before = await locator.evaluate((node) => node.innerText || node.value || '');
        const trigger = definition.triggerSelectors?.length
          ? await firstVisible(page, definition.triggerSelectors)
          : null;
        const triggerClicked = trigger
          ? await trigger.click({ timeout: 1500 }).then(() => true).catch(() => false)
          : (!definition.strictTriggerSelectors && Boolean(await clickNamed(page, definition.triggerNames || [])));
        if (!triggerClicked) {
          missing.push(tag);
          continue;
        }
        if (definition.triggerSettleMs) await page.waitForTimeout(definition.triggerSettleMs);
        let after = await locator.evaluate((node) => node.innerText || node.value || '');
        markerDiagnostics = topicMarkerDiagnostics(before, after);
        markerDiagnostics.selectionAfterTrigger = await topicSelectionDiagnostics(locator);
        const placeholderLength = trailingTopicPlaceholderLength(after);
        for (let index = 0; index < placeholderLength; index += 1) {
          await keyboard.press('Backspace');
        }
        if (placeholderLength) {
          after = await locator.evaluate((node) => node.innerText || node.value || '');
          markerDiagnostics.afterPlaceholderTrim = topicMarkerDiagnostics(before, after).after;
        }
        const triggerInsertedMarker = hasTrailingTopicMarker(before, after);
        if (!triggerInsertedMarker && definition.requireTriggerMarker) {
          diagnostics.push({ tag, marker: markerDiagnostics, trigger: 'marker_missing' });
          missing.push(tag);
          continue;
        }
        // XHS owns the active suggestion range after its topic button inserts
        // "#". Refocusing the ProseMirror editor destroys that range and the
        // next key creates a second plain "#", so preserve the native caret.
        if (!triggerInsertedMarker) await focusEditorEnd(locator);
        const topicText = triggerInsertedMarker ? tag : `#${tag}`;
        if (triggerInsertedMarker && definition.preserveNativeFocus && definition.incrementalExactSuggestions) {
          // XHS updates its official topic results incrementally. Human-like
          // character pacing prevents a slower prior query from overwriting
          // the exact final suggestion before we click it.
          if (!await typeInlinePickerTopic(page, topicText, definition, false)) {
            diagnostics.push({ tag, marker: markerDiagnostics, topicProvider: 'exact_suggestion_missing' });
            missing.push(tag);
            await rollbackUncommittedTopic(tag);
            continue;
          }
        } else if (triggerInsertedMarker && definition.preserveNativeFocus) {
          await keyboard.type(topicText, { delay: definition.typingDelayMs || 30 });
        } else {
          await locator.type(topicText, { delay: definition.typingDelayMs || 30 });
        }
        await page.waitForTimeout(definition.settleMs || 300);
        markerDiagnostics.selectionAfterType = await topicSelectionDiagnostics(locator);
        if (!await clickExactTopicSuggestion(page, tag)) {
          const candidates = await inspectTopicSuggestions(page, tag);
          if (definition.preserveNativeFocus) {
            await keyboard.press(definition.commitKey || 'Enter');
          } else {
            await locator.press(definition.commitKey || 'Enter');
          }
          await page.waitForTimeout(300);
          if (!await isSelectedTopic(locator, tag)) {
            diagnostics.push({ tag, marker: markerDiagnostics, candidates });
            missing.push(tag);
            await rollbackUncommittedTopic(tag);
            continue;
          }
        }
      } else if (definition.mode === 'inline_picker') {
        if (definition.preserveNativeFocus) {
          if (!await typeInlinePickerTopic(page, tag, definition)) {
            diagnostics.push({ tag, topicProvider: 'exact_suggestion_missing' });
            missing.push(tag);
            break;
          }
        } else {
          await locator.type(`#${tag}`, { delay: definition.typingDelayMs || 30 });
        }
        await page.waitForTimeout(definition.settleMs || 300);
        if (!await clickExactTopicSuggestion(page, tag)) {
          diagnostics.push({
            tag,
            candidates: await inspectTopicSuggestions(page, tag),
            editorTokens: await inspectTopicTokens(locator, tag),
          });
          missing.push(tag);
          break;
        }
      } else {
        await locator.type(`#${tag}`, { delay: 30 });
        if (definition.commitWithText) await locator.type(' ', { delay: 30 });
        else await keyboard.press('Space');
        await page.waitForTimeout(definition.settleMs || 250);
        if (!await isSelectedTopic(locator, tag)) {
          await clickExactTopicSuggestion(page, tag);
        }
      }
      await page.waitForTimeout(250);
      if (await isSelectedTopic(locator, tag)) {
        selected.push(tag);
        await locator.press('Escape').catch(() => {});
      } else {
        diagnostics.push({
          tag,
          marker: markerDiagnostics,
          candidates: await inspectTopicSuggestions(page, tag),
          editorTokens: await inspectTopicTokens(locator, tag),
        });
        missing.push(tag);
        await rollbackUncommittedTopic(tag);
      }
    }
  } catch (_) {
    return { selected, missing: normalized.filter((tag) => !selected.includes(tag)), diagnostics };
  }
  if (definition.exactNativeTopicSet) {
    let native = await nativeTopicSet(locator, definition);
    if (!native.invalid && !exactTopicSetMatches(native.topics, normalized)
      && definition.removeUnexpectedNativeTopics) {
      const reconciled = await reconcileNativeTopicSet(page, locator, normalized, definition);
      native = reconciled.native;
      if (reconciled.removed.length && !native.invalid && exactTopicSetMatches(native.topics, normalized)) {
        diagnostics.push({ topicSet: 'normalized', removed: reconciled.removed });
      }
    }
    if (native.invalid || !exactTopicSetMatches(native.topics, normalized)) {
      diagnostics.push({
        topicSet: 'mismatch',
        requested: normalized,
        actual: native.topics,
        invalidNativeTopic: native.invalid,
      });
      // A complete-but-wrong set (extra or duplicate topics) must block the
      // final submit just like a missing requested topic.
      return { selected, missing: normalized, diagnostics };
    }
    if (!await exactBodyWithoutNativeTopics(locator, definition, expectedBody)) {
      diagnostics.push({ topicSet: 'mismatch', bodyWithoutNativeTopics: 'mismatch' });
      return { selected, missing: normalized, diagnostics };
    }
    return { selected: normalized, missing: [], diagnostics };
  }
  return { selected, missing: selected.length ? [] : missing, diagnostics };
}

async function uniqueFileInput(page, kind) {
  // A type-specific, unique file input is the only safe generic upload
  // contract. Missing or ambiguous controls fail closed below.
  const matches = page.locator(`input[type='file'][accept*='${kind}' i]`);
  const count = await matches.count();
  if (count === 1) return { locator: matches.first(), count };
  // Kuaishou keeps a single-image cover input beside its multi-image work
  // uploader. For a frozen graph post, the unique multi-image input is the
  // reviewed target; do not guess between the two generic image inputs.
  if (kind === 'image' && count > 1) {
    const multiple = page.locator(`input[type='file'][accept*='${kind}' i][multiple]`);
    const multipleCount = await multiple.count();
    if (multipleCount === 1) return { locator: multiple.first(), count: multipleCount };
  }
  // XHS advertises its reviewed video control as an extension list instead
  // of video/* (for example .mp4,.mov). Accept that format only when it is
  // still uniquely identifiable; any ambiguity stays fail-closed.
  if (kind !== 'video' || count !== 0) return { locator: null, count };
  const extensions = page.locator("input[type='file'][accept*='.mp4' i]");
  const extensionCount = await extensions.count();
  if (extensionCount !== 1) return { locator: null, count: extensionCount };
  return { locator: extensions.first(), count: extensionCount };
}

async function setBooleanControl(locator, expected, clickControl = null) {
  try {
    const tag = await locator.evaluate((node) => node.tagName.toLowerCase());
    const type = (await locator.getAttribute('type') || '').toLowerCase();
    if (tag === 'input' && ['checkbox', 'radio'].includes(type)) {
      if (type === 'radio' && !expected) return false;
      if (await locator.isChecked() === expected) return true;
      if (clickControl) await clickControl.click({ timeout: 1500 });
      else await locator.setChecked(expected, { timeout: 1500 });
      return await locator.isChecked() === expected;
    }
    const before = await booleanControlState(locator);
    if (before === null) return false;
    if (before !== expected) await (clickControl || locator).click({ timeout: 1500 });
    return await booleanControlState(locator) === expected;
  } catch (_) {
    return false;
  }
}

async function booleanControlState(locator) {
  try {
    const tag = await locator.evaluate((node) => node.tagName.toLowerCase());
    const type = (await locator.getAttribute('type') || '').toLowerCase();
    if (tag === 'input' && ['checkbox', 'radio'].includes(type)) return await locator.isChecked();
    for (const attribute of ['aria-checked', 'aria-pressed']) {
      const value = await locator.getAttribute(attribute);
      if (value === 'true' || value === 'false') return value === 'true';
    }
    return null;
  } catch (_) {
    return null;
  }
}

async function choiceControlState(locator) {
  const booleanState = await booleanControlState(locator);
  if (booleanState !== null) return booleanState;
  try {
    const tag = await locator.evaluate((node) => node.tagName.toLowerCase());
    if (tag === 'option') return await locator.evaluate((node) => node.selected === true);
    const selected = await locator.getAttribute('aria-selected');
    if (selected === 'true' || selected === 'false') return selected === 'true';
    const dataSelected = await locator.getAttribute('data-selected');
    if (dataSelected === 'true' || dataSelected === 'false') return dataSelected === 'true';
    return null;
  } catch (_) {
    return null;
  }
}

async function uniqueStateControl(page, selectors) {
  if (!selectors?.length) return { control: null, ambiguous: false };
  const controls = page.locator(selectors.join(','));
  const count = await controls.count();
  if (count !== 1) return { control: null, ambiguous: count > 1 };
  return { control: controls.first(), ambiguous: false };
}

async function uniqueScopedBooleanControl(page, definition) {
  const wrappers = page.locator(definition.wrapperSelector);
  const count = Math.min(await wrappers.count(), 21);
  let wrapper = null;
  for (let index = 0; index < count; index += 1) {
    const candidate = wrappers.nth(index);
    if (!await candidate.isVisible().catch(() => false)) continue;
    const exactLabel = await candidate.evaluate((node, scoped) => {
      const normalized = (value) => String(value || '').normalize('NFKC').replace(/\s+/gu, ' ').trim();
      return [...node.querySelectorAll(scoped.selector)]
        .some((label) => normalized(label.textContent) === normalized(scoped.label));
    }, { selector: definition.labelSelector, label: definition.scopedLabel }).catch(() => false);
    if (!exactLabel) continue;
    if (wrapper) return { control: null, clickControl: null, ambiguous: true };
    wrapper = candidate;
  }
  if (!wrapper) return { control: null, clickControl: null, ambiguous: false };
  const stateControls = wrapper.locator(definition.stateSelector);
  const stateCount = await stateControls.count();
  if (stateCount !== 1) {
    return { control: null, clickControl: null, ambiguous: stateCount > 1 };
  }
  const clickControls = wrapper.locator(definition.clickSelector);
  const clickCount = await clickControls.count();
  const clickControl = clickCount === 1 && await clickControls.first().isVisible().catch(() => false)
    ? clickControls.first()
    : null;
  return { control: stateControls.first(), clickControl, ambiguous: clickCount > 1 };
}

async function uniqueNearbyBooleanControl(page, definition) {
  let control = null;
  let clickControl = null;
  let controlIdentity = null;
  for (const name of definition.nearbyLabelNames || []) {
    const labels = page.getByText(name, { exact: true });
    const count = Math.min(await labels.count(), 21);
    for (let index = 0; index < count; index += 1) {
      const label = labels.nth(index);
      if (!await label.isVisible().catch(() => false)) continue;
      // TikTok changes the final submit button text to "Schedule/预约发布".
      // It is not a settings label and must not make the nearby radio lookup
      // ambiguous after native scheduling has been enabled.
      const insideButton = await label.evaluate(
        (node) => Boolean(node.closest('button,[role="button"]')),
      ).catch(() => false);
      if (insideButton) continue;
      let scope = label;
      let nearby = null;
      for (let depth = 0; depth < 5; depth += 1) {
        const states = scope.locator(definition.nearbyStateSelector);
        const stateCount = await states.count();
        if (stateCount > 1) {
          const labelBox = await label.boundingBox().catch(() => null);
          const aligned = [];
          for (let stateIndex = 0; labelBox && stateIndex < stateCount; stateIndex += 1) {
            const box = await states.nth(stateIndex).evaluate((node) => {
              let current = node;
              for (let ancestor = 0; current && ancestor < 4; ancestor += 1, current = current.parentElement) {
                const rect = current.getBoundingClientRect();
                if (rect.width > 0 && rect.height > 0) {
                  return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
                }
              }
              return null;
            }).catch(() => null);
            if (!box) continue;
            const labelCenter = labelBox.y + labelBox.height / 2;
            const stateCenter = box.y + box.height / 2;
            if (Math.abs(labelCenter - stateCenter) <= Math.max(48, labelBox.height * 2)) {
              aligned.push(stateIndex);
            }
          }
          if (aligned.length === 1) {
            nearby = states.nth(aligned[0]);
            break;
          }
          return { control: null, clickControl: null, ambiguous: true };
        }
        if (stateCount === 1) {
          nearby = states.first();
          break;
        }
        scope = scope.locator('xpath=..');
      }
      if (!nearby) continue;
      const identity = await nearby.evaluate((node, selector) => (
        [...document.querySelectorAll(selector)].indexOf(node)
      ), definition.nearbyStateSelector).catch(() => -1);
      if (control) {
        if (identity === controlIdentity) continue;
        return { control: null, clickControl: null, ambiguous: true };
      }
      control = nearby;
      controlIdentity = identity;
      // The platform often makes the reviewed visible label the click target
      // while keeping its native input visually hidden.
      clickControl = label;
    }
  }
  return { control, clickControl, ambiguous: false };
}

async function uniqueNearbyValueControl(page, labelNames, selector, maxDepth = 5) {
  let control = null;
  let identity = null;
  for (const name of labelNames || []) {
    const labels = page.getByText(name, { exact: true });
    const count = Math.min(await labels.count(), 21);
    for (let index = 0; index < count; index += 1) {
      const label = labels.nth(index);
      if (!await label.isVisible().catch(() => false)) continue;
      let scope = label;
      let nearby = null;
      for (let depth = 0; depth < maxDepth; depth += 1) {
        const candidates = scope.locator(selector);
        const visible = [];
        for (let candidateIndex = 0; candidateIndex < await candidates.count(); candidateIndex += 1) {
          const candidate = candidates.nth(candidateIndex);
          if (await candidate.isVisible().catch(() => false)) visible.push(candidate);
        }
        if (visible.length > 1) return { control: null, ambiguous: true };
        if (visible.length === 1) {
          nearby = visible[0];
          break;
        }
        scope = scope.locator('xpath=..');
      }
      if (!nearby) continue;
      const nextIdentity = await nearby.evaluate((node, reviewedSelector) => (
        [...document.querySelectorAll(reviewedSelector)].indexOf(node)
      ), selector).catch(() => -1);
      if (control) {
        if (nextIdentity === identity) continue;
        return { control: null, ambiguous: true };
      }
      control = nearby;
      identity = nextIdentity;
    }
  }
  return { control, ambiguous: false };
}

async function xhsComposerLandmarksVerified(page, contentType) {
  if (!page.url().startsWith(PLATFORM_RULES.xhs.composerUrl)) return false;
  const publicState = await enumSelectedTextState(
    page,
    PLATFORM_RULES.xhs.settingControls.visibility.options.public,
  );
  if (publicState.ambiguous || !publicState.selected) return false;
  const declaration = await uniqueVisible(page, [
    '.publish-page-content-setting-content .custom-select-44 .d-select-content',
  ]);
  if (declaration.ambiguous || !declaration.control) return false;
  const declarationText = normalizedSettingText(await declaration.control.innerText().catch(() => ''));
  if (!new Set([
    '添加内容类型声明',
    '笔记含AI合成内容',
    '笔记含 AI 合成内容',
  ].map(normalizedSettingText)).has(declarationText)) return false;
  if (contentType === 'video') {
    const title = await uniqueVisible(page, PLATFORM_RULES.xhs.title);
    const body = await uniqueVisible(page, PLATFORM_RULES.xhs.body);
    const submit = await firstReviewedSubmit(page, PLATFORM_RULES.xhs, 0);
    const schedule = await uniqueVisibleExactText(page, ['定时发布']);
    const nativeCover = await uniqueVisibleExactText(
      page,
      PLATFORM_RULES.xhs.coverControl.nativeDefaultNames,
    );
    return !title.ambiguous && Boolean(title.control)
      && !body.ambiguous && Boolean(body.control)
      && Boolean(submit)
      && !schedule.ambiguous && Boolean(schedule.control)
      && !nativeCover.ambiguous && Boolean(nativeCover.control);
  }
  for (const key of ['allow_duet', 'allow_copy']) {
    const match = await uniqueScopedBooleanControl(page, PLATFORM_RULES.xhs.settingControls[key]);
    if (match.ambiguous || !match.control || !match.clickControl) return false;
    if (await booleanControlState(match.control) === null) return false;
  }
  return true;
}

async function douyinGraphTextComposerLandmarksVerified(page) {
  const prefixes = PLATFORM_RULES.douyin.entryFlows?.graph_text?.requiredURLPrefixes || [];
  if (!prefixes.some((prefix) => page.url().startsWith(prefix))) return false;
  const title = await uniqueVisible(page, ["input[placeholder='添加作品标题']"]);
  const body = await uniqueVisible(page, ["div[contenteditable='true']"]);
  const submit = await uniqueVisibleNamedControl(page, ['发布'], ['button']);
  if (title.ambiguous || !title.control || body.ambiguous || !body.control
    || submit.ambiguous || !submit.control) return false;
  for (const name of ['公开', '好友可见', '仅自己可见', '允许', '不允许', '立即发布', '定时发布']) {
    const match = await uniqueVisibleNamedControl(page, [name], ['checkbox']);
    if (match.ambiguous || !match.control || await booleanControlState(match.control) === null) return false;
  }
  const declaration = await uniqueVisible(page, ['.selectText-XSrMFZ']);
  if (declaration.ambiguous || !declaration.control) return false;
  const text = normalizedSettingText(await declaration.control.innerText().catch(() => ''));
  return ['请选择自主声明', '无需添加自主声明', '内容由AI生成'].includes(text);
}

async function douyinVideoComposerLandmarkState(page) {
  const reviewedURLPrefixes = [
    PLATFORM_RULES.douyin.composerUrl,
    ...(PLATFORM_RULES.douyin.composerURLPrefixes?.video || []),
  ];
  const state = {
    url: reviewedURLPrefixes.some((prefix) => page.url().startsWith(prefix)),
    title: false,
    body: false,
    submit: false,
    public: null,
    downloadDisabled: null,
    immediate: null,
    scheduled: null,
    declaration: false,
  };
  const title = await uniqueVisible(page, PLATFORM_RULES.douyin.title);
  const body = await uniqueVisible(page, PLATFORM_RULES.douyin.body);
  const submit = await uniqueVisibleNamedControl(page, ['发布'], ['button']);
  state.title = !title.ambiguous && Boolean(title.control);
  state.body = !body.ambiguous && Boolean(body.control);
  state.submit = !submit.ambiguous && Boolean(submit.control);
  const reviewedChoice = async (name) => {
    const match = await uniqueVisibleNamedControl(page, [name], ['radio', 'checkbox']);
    if (match.ambiguous || !match.control) return null;
    return booleanControlState(match.control);
  };
  state.public = await reviewedChoice('公开');
  state.downloadDisabled = await reviewedChoice('不允许');
  state.immediate = await reviewedChoice('立即发布');
  state.scheduled = await reviewedChoice('定时发布');
  const declaration = await uniqueVisible(page, ['.selectText-XSrMFZ']);
  if (!declaration.ambiguous && declaration.control) {
    const text = normalizedSettingText(await declaration.control.innerText().catch(() => ''));
    state.declaration = ['请选择自主声明', '无需添加自主声明', '内容由AI生成'].includes(text);
  }
  return state;
}

async function douyinVideoComposerLandmarksVerified(page) {
  const state = await douyinVideoComposerLandmarkState(page);
  return state.url && state.title && state.body && state.submit
    && state.public === true && state.downloadDisabled === true
    && state.immediate !== null && state.scheduled !== null
    && state.immediate !== state.scheduled && state.declaration;
}

async function kuaishouComposerLandmarksVerified(page) {
  const body = await uniqueVisible(page, PLATFORM_RULES.kuaishou.body);
  const submit = await uniqueVisibleExactText(page, ['发布']);
  if (body.ambiguous || !body.control || submit.ambiguous || !submit.control) return false;
  for (const name of [
    '允许别人跟我拍同框', '允许下载此作品', '作品展示在同城页',
    '所有人可见', '好友可见', '仅自己可见', '立即发布', '定时发布',
  ]) {
    const match = await uniqueVisibleExactText(page, [name]);
    if (match.ambiguous || !match.control) return false;
  }
  return true;
}

async function tiktokVideoComposerLandmarksVerified(page) {
  const state = await tiktokVideoComposerLandmarkState(page);
  return Object.values(state).every(Boolean);
}

async function tiktokVideoComposerLandmarkState(page) {
  const state = {
    url: page.url().startsWith(PLATFORM_RULES.tiktok.composerUrl),
    body: false,
    submit: false,
    visibility: false,
    comments: false,
    reuse: false,
    now: false,
    scheduled: false,
    ai: false,
  };
  const body = await uniqueVisible(page, PLATFORM_RULES.tiktok.body);
  const submit = await uniqueVisibleNamedControl(page, ['Post', '发布', 'Schedule', '预约发布'], ['button']);
  state.body = !body.ambiguous && Boolean(body.control);
  state.submit = !submit.ambiguous && Boolean(submit.control);
  const visibility = await enumSelectedTextState(
    page,
    PLATFORM_RULES.tiktok.settingControls.visibility.options.public,
  );
  state.visibility = !visibility.ambiguous && visibility.selected;
  const comments = await uniqueVisibleNamedControl(page, ['Comments', '评论'], ['checkbox']);
  const reuse = await uniqueVisibleNamedControl(page, ['Reuse content', '复用内容'], ['checkbox']);
  const now = await uniqueVisibleNamedControl(page, ['Now', '现在'], ['radio']);
  const scheduled = await uniqueVisibleNamedControl(page, ['Schedule', '定时发布', '预约发布'], ['radio']);
  state.comments = !comments.ambiguous && Boolean(comments.control)
    && await booleanControlState(comments.control) !== null;
  state.reuse = !reuse.ambiguous && Boolean(reuse.control)
    && await booleanControlState(reuse.control) !== null;
  state.now = !now.ambiguous && Boolean(now.control)
    && await booleanControlState(now.control) !== null;
  state.scheduled = !scheduled.ambiguous && Boolean(scheduled.control)
    && await booleanControlState(scheduled.control) !== null;
  const ai = await uniqueNearbyBooleanControl(page, PLATFORM_RULES.tiktok.settingControls.ai_generated);
  state.ai = !ai.ambiguous && Boolean(ai.control)
    && await booleanControlState(ai.control) !== null;
  return state;
}

async function verifiedAbsentComposer(page, rule, contentType) {
  if (rule.verifiedAbsentComposer === 'xhs') return xhsComposerLandmarksVerified(page, contentType);
  if (rule.verifiedAbsentComposer === 'douyin') {
    if (contentType === 'graph_text') return douyinGraphTextComposerLandmarksVerified(page);
    if (contentType === 'video') return douyinVideoComposerLandmarksVerified(page);
    return false;
  }
  if (rule.verifiedAbsentComposer === 'kuaishou') return kuaishouComposerLandmarksVerified(page);
  if (rule.verifiedAbsentComposer === 'tiktok_video') return tiktokVideoComposerLandmarksVerified(page);
  return false;
}

async function verifiedAbsentBooleanSetting(page, rule, definition, value, contentType) {
  if (!rule.verifiedAbsentDefaults || value !== definition.verifiedAbsentValue) return false;
  const selectedControl = await uniqueVisible(page, definition.selectors || []);
  if (selectedControl.ambiguous || selectedControl.control) return false;
  const namedControl = await uniqueVisibleExactText(page, definition.names || []);
  if (namedControl.ambiguous || namedControl.control) return false;
  return verifiedAbsentComposer(page, rule, contentType);
}

async function verifiedAbsentEnumSetting(page, rule, definition, value, contentType) {
  if (!rule.verifiedAbsentDefaults || value !== definition.verifiedAbsentValue) return false;
  for (const selector of definition.triggerSelectors || []) {
    if (await page.locator(selector).count()) return false;
  }
  for (const name of [
    ...(definition.triggerNames || []),
    ...Object.values(definition.options || {}).flatMap((option) => option.names || []),
  ]) {
    if (await page.getByText(name, { exact: true }).count()) return false;
  }
  return verifiedAbsentComposer(page, rule, contentType);
}

async function firstVisibleNamedControl(page, names, roles) {
  for (const name of names || []) {
    for (const role of roles) {
      const candidate = page.getByRole(role, { name, exact: true }).first();
      if (await candidate.count() && await candidate.isVisible().catch(() => false)) return candidate;
    }
  }
  return null;
}

async function clickExactTrigger(page, names) {
  const control = await firstVisibleNamedControl(page, names, ['button', 'combobox']);
  if (control) {
    return await control.click({ timeout: 1500 }).then(() => true).catch(() => false);
  }
  for (const name of names || []) {
    const text = page.getByText(name, { exact: true }).first();
    if (await text.count() && await text.isVisible().catch(() => false)) {
      return await text.click({ timeout: 1500 }).then(() => true).catch(() => false);
    }
  }
  return false;
}

function normalizedFuzzyText(value) {
  return String(value || '').normalize('NFKC').toLocaleLowerCase().replace(/[^\p{L}\p{N}]+/gu, '');
}

function fuzzyLocationTokens(value) {
  const characters = [...normalizedFuzzyText(value)];
  if (characters.length <= 2) return characters.length ? [characters.join('')] : [];
  const tokens = [];
  for (let index = 0; index < characters.length - 1; index += 2) {
    tokens.push(`${characters[index]}${characters[index + 1]}`);
  }
  return [...new Set(tokens)];
}

async function firstFuzzyOption(page, value, definition) {
  const expected = normalizedFuzzyText(value);
  if (!expected) return null;
  const expectedTokens = fuzzyLocationTokens(value);
  const minimumMatches = definition.minimumFuzzyTokenMatches || 0;
  const selectors = definition.optionSelectors || ["[role='listbox'] [role='option']", "[role='option']"];
  const deadline = Date.now() + (definition.optionTimeoutMs || 5000);
  let fallback = null;
  do {
    let firstVisibleOption = null;
    for (const selector of selectors) {
      const options = page.locator(selector);
      const count = Math.min(await options.count(), 50);
      for (let index = 0; index < count; index += 1) {
        const option = options.nth(index);
        if (!await option.isVisible().catch(() => false)) continue;
        if (!firstVisibleOption) {
          firstVisibleOption = option;
          fallback = option;
        }
        const actual = normalizedFuzzyText(await option.innerText().catch(() => ''));
        if (actual.includes(expected)) return option;
        if (definition.chooseFirstResult && minimumMatches
          && expectedTokens.filter((token) => actual.includes(token)).length >= minimumMatches) return option;
      }
    }
    if (definition.chooseFirstResult && firstVisibleOption && !minimumMatches) return firstVisibleOption;
    if (Date.now() >= deadline) return definition.chooseFirstResult ? fallback : null;
    await page.waitForTimeout(100);
  } while (true);
}

async function typeSettingValue(page, control, value, definition = {}) {
  try {
    if (definition.fillDirectly) {
      await control.fill(value, { timeout: 1500 });
      return await control.inputValue() === value;
    }
    await control.click({ timeout: 1500 });
    const keyboard = keyboardFor(page);
    await keyboard.press(process.platform === 'darwin' ? 'Meta+A' : 'Control+A');
    await keyboard.type(value, { delay: 35 });
    // ponytail: Semi Select owns a hidden native editor; the following
    // official-option click is the authoritative commit check.
    if (definition.keyboardFilterable) return true;
    const tag = await control.evaluate((node) => node.tagName.toLowerCase());
    if (tag === 'input' || tag === 'textarea') return await control.inputValue() === value;
    return contentEditableValueMatches(await control.innerText(), value);
  } catch (_) {
    return false;
  }
}

async function firstVisibleSettingTrigger(page, definition) {
  const bySelector = await firstVisible(page, definition.triggerSelectors || []);
  if (bySelector) return bySelector;
  const byRole = await firstVisibleNamedControl(page, definition.triggerNames || [], ['button', 'combobox']);
  if (byRole) return byRole;
  for (const name of definition.triggerNames || []) {
    const exact = page.getByText(name, { exact: true }).first();
    if (await exact.count() && await exact.isVisible().catch(() => false)) return exact;
  }
  return null;
}

async function uniqueVisibleSettingTrigger(page, definition) {
  const bySelector = await uniqueVisible(page, definition.triggerSelectors || []);
  if (bySelector.ambiguous || bySelector.control) return bySelector;
  const byRole = await uniqueVisibleNamedControl(page, definition.triggerNames || [], ['button', 'combobox']);
  if (byRole.ambiguous || byRole.control) return byRole;
  let control = null;
  for (const name of definition.triggerNames || []) {
    const matches = page.getByText(name, { exact: true });
    const count = Math.min(await matches.count(), 21);
    for (let index = 0; index < count; index += 1) {
      const candidate = matches.nth(index);
      if (!await candidate.isVisible().catch(() => false)) continue;
      if (control) return { control: null, ambiguous: true };
      control = candidate;
    }
  }
  return { control, ambiguous: false };
}

async function textSettingValue(control) {
  try {
    const tag = await control.evaluate((node) => node.tagName.toLowerCase());
    if (tag === 'input' || tag === 'textarea') return await control.inputValue();
    return await control.innerText();
  } catch (_) {
    return null;
  }
}

async function textSettingHasCommittedSelection(page, control, definition) {
  const scope = definition.selectedTextAncestorXPath
    ? control.locator(`xpath=${definition.selectedTextAncestorXPath}`)
    : page;
  for (const selector of definition.selectedTextSelectors || []) {
    const selected = await firstVisible(scope, [selector]);
    if (selected && String(await selected.innerText().catch(() => '')).trim()) return true;
  }
  return false;
}

async function clearTextSetting(page, control, definition) {
  if (await textSettingHasCommittedSelection(page, control, definition)) return 'unverified';
  const current = await textSettingValue(control);
  if (current === null) return 'unverified';
  if (!String(current).trim()) return 'verified';
  try {
    await control.fill('', { timeout: 1500 });
    await page.waitForTimeout(100);
  } catch (_) {
    return 'unverified';
  }
  if (await textSettingHasCommittedSelection(page, control, definition)) return 'unverified';
  const cleared = await textSettingValue(control);
  return cleared !== null && !String(cleared).trim() ? 'applied' : 'unverified';
}

async function fuzzyOptionSelectionCommitted(page, control, option, value, definition = {}) {
  const expected = normalizedFuzzyText(value);
  const expectedTokens = fuzzyLocationTokens(value);
  const selectedTextMinimumMatches = definition.selectedTextMinimumFuzzyTokenMatches || 0;
  const requiresSelectedTextMatch = Boolean(
    definition.requireSelectedTextCommit && definition.selectedTextSelectors?.length,
  );
  const deadline = Date.now() + 1500;
  do {
    const optionSelected = await option.evaluate((node) => {
      const className = String(node.className || '').toLowerCase();
      return node.getAttribute('aria-selected') === 'true'
        || node.getAttribute('data-selected') === 'true'
        || /(^|[-_])(selected|active|checked)([-_]|$)/.test(className);
    }).catch(() => false);
    if (optionSelected && !requiresSelectedTextMatch) return true;
    const controlState = await control.evaluate((node) => ({
      expanded: node.getAttribute('aria-expanded'),
      selected: node.getAttribute('aria-selected') === 'true' || node.getAttribute('data-selected') === 'true',
      value: 'value' in node ? node.value : node.textContent,
    })).catch(() => ({ expanded: null, selected: false, value: '' }));
    if (controlState.selected && !requiresSelectedTextMatch) return true;
    const selectedTextScope = definition.selectedTextAncestorXPath
      ? control.locator(`xpath=${definition.selectedTextAncestorXPath}`)
      : page;
    for (const selector of definition.selectedTextSelectors || []) {
      const selected = await firstVisible(selectedTextScope, [selector]);
      const selectedText = selected
        ? normalizedFuzzyText(await selected.innerText().catch(() => ''))
        : '';
      if (selectedText && (
        !definition.selectedTextMustMatchValue
        || selectedText.includes(expected)
        || (selectedTextMinimumMatches > 0
          && expectedTokens.filter((token) => selectedText.includes(token)).length >= selectedTextMinimumMatches)
      )) return true;
    }
    const valueMatches = normalizedFuzzyText(controlState.value).includes(expected);
    const optionVisible = await option.isVisible().catch(() => false);
    const controlVisible = await control.isVisible().catch(() => false);
    if (!requiresSelectedTextMatch) {
      if (definition.chooseFirstResult && !optionVisible && normalizedFuzzyText(controlState.value)) return true;
      if ((valueMatches && (controlState.expanded === 'false' || !optionVisible))
        || (!controlVisible && !optionVisible)) return true;
    }
    await page.waitForTimeout(100);
  } while (Date.now() < deadline);
  return false;
}

async function applyTextSetting(page, definition, value) {
  const emptyRequested = !value.trim();
  if (emptyRequested && definition.emptyReadback) {
    const readback = await emptyTextSettingReadbackState(page, definition);
    if (readback === 'verified') return 'verified';
    if (readback === 'mismatch') return 'unverified';
  }
  if (emptyRequested && definition.emptyChoiceNames?.length) {
    const trigger = await firstVisibleSettingTrigger(page, definition);
    if (!trigger
      || !await trigger.click({ timeout: 1500 }).then(() => true).catch(() => false)) return 'unverified';
    const choice = await uniqueVisibleExactText(
      page,
      definition.emptyChoiceNames,
      definition.emptyChoiceSelectors || [],
    );
    if (choice.ambiguous || !choice.control
      || !await choice.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) return 'unverified';
    await page.waitForTimeout(100);
    return await emptyTextSettingReadback(page, definition) ? 'applied' : 'unverified';
  }
  if (!emptyRequested && (definition.regionTriggerSelectors?.length || definition.regionTriggerNames?.length)) {
    let regionTrigger = await firstVisible(page, definition.regionTriggerSelectors || []);
    if (!regionTrigger) regionTrigger = await firstVisibleNamedControl(page, definition.regionTriggerNames, ['button', 'combobox']);
    if (!regionTrigger) {
      for (const name of definition.regionTriggerNames) {
        const text = page.getByText(name, { exact: true }).first();
        if (await text.count() && await text.isVisible().catch(() => false)) {
          regionTrigger = text;
          break;
        }
      }
    }
    if (regionTrigger) {
      if (!await regionTrigger.click({ timeout: 1500 }).then(() => true).catch(() => false)) return 'unverified';
      const reviewedRegionOption = await firstVisibleEventually(page, definition.regionOptionSelectors || [], 5000);
      const regionSelected = reviewedRegionOption
        ? await reviewedRegionOption.click({ timeout: 1500 }).then(() => true).catch(() => false)
        : Boolean(await clickEntryNamedEventually(page, definition.regionOptionNames || [], 5000));
      if (!regionSelected) return 'unverified';
      await page.waitForTimeout(300);
    }
  }
  let control = await firstVisibleEventually(page, definition.selectors || [], emptyRequested ? 250 : 1500);
  let triggerSeen = false;
  if (!control && (definition.triggerSelectors?.length || definition.triggerNames?.length)) {
    const trigger = await firstVisibleSettingTrigger(page, definition);
    triggerSeen = Boolean(trigger);
    if (trigger && !await trigger.click({ timeout: 1500 }).then(() => true).catch(() => false)) return 'unverified';
    control = await firstVisibleEventually(page, definition.selectors || [], definition.chooseFuzzy ? 5000 : 2000);
  }
  if (!control) return emptyRequested && !triggerSeen ? 'not_present' : 'unverified';
  if (emptyRequested) return await clearTextSetting(page, control, definition);
  const filled = definition.chooseFuzzy
    ? await typeSettingValue(page, control, value, definition)
    : await fillLocator(control, value);
  if (!filled) return 'unverified';
  if (definition.chooseFuzzy) {
    const option = await firstFuzzyOption(page, value, definition);
    if (!option || !await option.click({ timeout: 1500 }).then(() => true).catch(() => false)) return 'unverified';
    return await fuzzyOptionSelectionCommitted(page, control, option, value, definition) ? 'applied' : 'unverified';
  }
  if (!definition.chooseExact) return 'applied';
  const option = await firstVisibleNamedControl(page, [value], ['option']);
  if (option) {
    await option.click({ timeout: 1500 }).catch(() => {});
    return 'applied';
  }
  const exact = page.getByText(value, { exact: true }).first();
  if (!await exact.count() || !await exact.isVisible().catch(() => false)) return 'unverified';
  await exact.click({ timeout: 1500 }).catch(() => {});
  return 'applied';
}

function normalizedSettingText(value) {
  return String(value || '').normalize('NFKC').replace(/\s+/gu, ' ').trim();
}

function youtubeManagerTitleCandidates(value) {
  const exact = String(value || '').trim();
  const asciiSeparator = exact.replaceAll('｜', '|');
  // YouTube Studio renders this brand separator with surrounding spaces.
  // Keep the exception exact and local to YouTube manager verification.
  return [...new Set([
    exact,
    asciiSeparator,
    asciiSeparator.replace(/\s*\|\s*/gu, ' | '),
  ].filter(Boolean))];
}

async function youtubeManagerRowEvidence(root, titles) {
  const expected = new Set(titles.map(normalizedSettingText));
  const rows = root.locator('ytcp-video-row, tr, article');
  let matchingTitle = false;
  for (let rowIndex = 0; rowIndex < Math.min(await rows.count(), 100); rowIndex += 1) {
    const row = rows.nth(rowIndex);
    if (!await row.isVisible().catch(() => false)) continue;
    const titleControls = row.locator('#video-title, [id="video-title"], a[title], h1, h2');
    let rowTitleMatches = false;
    let matchingTitleControl = null;
    for (let titleIndex = 0; titleIndex < Math.min(await titleControls.count(), 10); titleIndex += 1) {
      const titleControl = titleControls.nth(titleIndex);
      if (!await titleControl.isVisible().catch(() => false)) continue;
      const values = [
        await titleControl.innerText().catch(() => ''),
        await titleControl.getAttribute('title').catch(() => ''),
      ];
      if (values.some((value) => expected.has(normalizedSettingText(value)))) {
        rowTitleMatches = true;
        matchingTitleControl = titleControl;
        break;
      }
    }
    if (!rowTitleMatches) continue;
    matchingTitle = true;
    for (const label of ['Scheduled', '定时发布', '定时发布中', '定时发表', '待发布']) {
      const scheduled = row.getByText(label, { exact: true });
      for (let index = 0; index < Math.min(await scheduled.count(), 5); index += 1) {
        if (await scheduled.nth(index).isVisible().catch(() => false)) {
          return {
            matchingTitle: true, scheduledState: true, publishedState: false,
            observedStatus: '', postURL: '', externalPostID: '',
          };
        }
      }
    }
    for (const label of ['Public', 'Published', '公开', '已发布']) {
      const published = row.getByText(label, { exact: true });
      for (let index = 0; index < Math.min(await published.count(), 5); index += 1) {
        if (!await published.nth(index).isVisible().catch(() => false)) continue;
        const href = await matchingTitleControl?.getAttribute('href').catch(() => '') || '';
        let externalPostID = '';
        try {
          const reviewed = new URL(href, 'https://studio.youtube.com');
          if (reviewed.hostname === 'studio.youtube.com') {
            externalPostID = reviewed.pathname.match(/^\/video\/([A-Za-z0-9_-]{6,64})\/edit$/u)?.[1] || '';
          }
        } catch {
          externalPostID = '';
        }
        return {
          matchingTitle: true,
          scheduledState: false,
          publishedState: Boolean(externalPostID),
          observedStatus: externalPostID ? (label === '公开' ? 'Public' : label) : '',
          postURL: externalPostID ? `https://www.youtube.com/shorts/${externalPostID}` : '',
          externalPostID,
        };
      }
    }
  }
  return {
    matchingTitle, scheduledState: false, publishedState: false,
    observedStatus: '', postURL: '', externalPostID: '',
  };
}

async function deleteYouTubeManagerContent(page, input) {
  const expected = new Set(youtubeManagerTitleCandidates(input.content?.title || '').map(normalizedSettingText));
  const rows = page.locator('ytcp-video-row');
  let target = null;
  for (let rowIndex = 0; rowIndex < Math.min(await rows.count(), 100); rowIndex += 1) {
    const row = rows.nth(rowIndex);
    if (!await row.isVisible().catch(() => false)) continue;
    const titleControls = row.locator('#video-title, [id="video-title"], a[title]');
    let titleMatches = false;
    for (let titleIndex = 0; titleIndex < Math.min(await titleControls.count(), 10); titleIndex += 1) {
      const title = titleControls.nth(titleIndex);
      const values = [
        await title.innerText().catch(() => ''),
        await title.getAttribute('title').catch(() => ''),
      ];
      if (values.some((value) => expected.has(normalizedSettingText(value)))) {
        titleMatches = true;
        break;
      }
    }
    if (!titleMatches) continue;
    const scheduled = await uniqueVisibleExactText(row, ['Scheduled', '定时发布', '定时发表', '待发布']);
    if (!scheduled.ambiguous && scheduled.control) {
      if (target) return { status: 'manual_takeover', step: 'delete_target_ambiguous', errorCode: 'FORM_REVIEW_REQUIRED', evidence: {} };
      target = row;
    }
  }
  if (!target) return { status: 'manual_takeover', step: 'delete_target_missing', errorCode: 'FORM_REVIEW_REQUIRED', evidence: {} };
  const menu = await firstVisible(target, [
    "button[aria-label='Options']",
    "button[aria-label='More actions']",
    "button[aria-label='更多操作']",
    '#overflow-menu',
  ]);
  if (!menu || !await menu.click({ timeout: 3000 }).then(() => true).catch(() => false)) {
    return { status: 'manual_takeover', step: 'delete_menu_missing', errorCode: 'FORM_REVIEW_REQUIRED', evidence: {} };
  }
  const deleteChoice = await uniqueVisibleExactText(page, ['Delete forever', '永久删除']);
  if (deleteChoice.ambiguous || !deleteChoice.control ||
      !await deleteChoice.control.click({ timeout: 3000 }).then(() => true).catch(() => false)) {
    return { status: 'manual_takeover', step: 'delete_choice_missing', errorCode: 'FORM_REVIEW_REQUIRED', evidence: {} };
  }
  const dialog = page.getByRole('dialog').filter({ visible: true }).last();
  if (!await dialog.count() || !await dialog.isVisible().catch(() => false)) {
    return { status: 'manual_takeover', step: 'delete_confirmation_missing', errorCode: 'FORM_REVIEW_REQUIRED', evidence: {} };
  }
  const checkbox = dialog.locator("input[type='checkbox']").first();
  if (await checkbox.count() && !await checkbox.isChecked().catch(() => false)) {
    await checkbox.check({ timeout: 3000 }).catch(() => {});
  }
  const confirm = await uniqueVisibleExactText(dialog, ['Delete forever', '永久删除'], ['button']);
  if (confirm.ambiguous || !confirm.control ||
      !await confirm.control.click({ timeout: 3000 }).then(() => true).catch(() => false)) {
    return { status: 'manual_takeover', step: 'delete_confirmation_failed', errorCode: 'FORM_REVIEW_REQUIRED', evidence: {} };
  }
  const deadline = Date.now() + Math.min(input.timeoutMs, 10000);
  while (Date.now() < deadline) {
    if (!await target.count() || await page.getByText('Content deleted', { exact: true }).isVisible().catch(() => false)) {
      const screenshotPath = await captureFailureScreenshot(page, 'youtube-deleted');
      return { status: 'deleted', step: 'creator_backend_deleted', errorCode: '', evidence: screenshotPath ? { screenshotPath } : {} };
    }
    await page.waitForTimeout(100);
  }
  return { status: 'manual_takeover', step: 'delete_outcome_unknown', errorCode: 'FORM_REVIEW_REQUIRED', evidence: {} };
}

async function enumSelectedTextState(page, option) {
  if (!option.selectedTextSelectors?.length) return { configured: false, selected: false, ambiguous: false };
  const match = await uniqueVisible(page, option.selectedTextSelectors);
  if (match.ambiguous) return { configured: true, selected: false, ambiguous: true };
  if (!match.control) return { configured: true, selected: false, ambiguous: false };
  const actual = normalizedSettingText(await match.control.innerText().catch(() => ''));
  const expected = (option.selectedTextValues || []).map(normalizedSettingText);
  return { configured: true, selected: expected.includes(actual), ambiguous: false };
}

async function enumSelectedTextStateEventually(page, option, timeoutMs = 1500) {
  const deadline = Date.now() + timeoutMs;
  do {
    const state = await enumSelectedTextState(page, option);
    if (state.ambiguous || state.selected || Date.now() >= deadline) return state;
    await page.waitForTimeout(100);
  } while (true);
}

async function confirmEnumSelection(page, option) {
  if (option.confirmNames?.length) {
    const confirm = await uniqueVisibleNamedControl(page, option.confirmNames, ['button']);
    if (confirm.ambiguous || !confirm.control) return false;
    if (!await confirm.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) return false;
  }
  if (!option.selectedTextSelectors?.length) return true;
  const state = await enumSelectedTextStateEventually(page, option);
  return !state.ambiguous && state.selected;
}

async function settleDouyinAIConfirmation(page, input) {
  if (input.platform !== 'douyin' || !['ai', 'none'].includes(input.settings.self_declaration)) return true;
  // The live console does not consistently expose a dialog role and nests
  // its heading text. Locate a visible exact heading, then use only the two
  // exact non-publish controls from that reviewed modal.
  const firstVisibleExactText = async (name) => {
    const candidates = page.getByText(name, { exact: true });
    const count = Math.min(await candidates.count(), 20);
    for (let index = 0; index < count; index += 1) {
      const candidate = candidates.nth(index);
      if (await candidate.isVisible().catch(() => false)) return candidate;
    }
    return null;
  };
  const title = await firstVisibleExactText('对作品内容添加声明');
  if (!title) return true;
  const choiceName = input.settings.self_declaration === 'ai' ? '内容由AI生成' : '无需添加自主声明';
  const radio = await uniqueVisibleNamedControl(page, [choiceName], ['radio']);
  if (radio.ambiguous) return false;
  if (radio.control) {
    let selected = await radio.control.isChecked().catch(() => null);
    if (selected === null) selected = await radio.control.getAttribute('aria-checked').then((value) => value === 'true').catch(() => false);
    if (!selected) {
      if (!await radio.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) return false;
      await page.waitForTimeout(100);
      selected = await radio.control.isChecked().catch(() => null);
      if (selected === null) selected = await radio.control.getAttribute('aria-checked').then((value) => value === 'true').catch(() => false);
      if (!selected) return false;
    }
  } else {
    const choice = await firstVisibleExactText(choiceName);
    if (!choice || !await choice.click({ timeout: 1500 }).then(() => true).catch(() => false)) return false;
  }
  const reviewedConfirm = await uniqueVisibleNamedControl(page, ['确定'], ['button']);
  if (reviewedConfirm.ambiguous) return false;
  const confirm = reviewedConfirm.control || await firstVisibleExactText('确定');
  if (!confirm || !await confirm.click({ timeout: 1500 }).then(() => true).catch(() => false)) return false;
  const deadline = Date.now() + 3000;
  do {
    if (!await firstVisibleExactText('对作品内容添加声明')) return true;
    await page.waitForTimeout(150);
  } while (Date.now() < deadline);
  return false;
}

async function uniqueVisibleExactText(page, names, selectors = []) {
  const expected = new Set((names || []).map(normalizedSettingText));
  const uniqueNames = [];
  const seenNames = new Set();
  for (const name of names || []) {
    const normalized = normalizedSettingText(name);
    if (seenNames.has(normalized)) continue;
    seenNames.add(normalized);
    uniqueNames.push(name);
  }
  let control = null;
  const inspect = async (candidates) => {
    const count = Math.min(await candidates.count(), 100);
    for (let index = 0; index < count; index += 1) {
      const candidate = candidates.nth(index);
      if (!await candidate.isVisible().catch(() => false)) continue;
      if (!expected.has(normalizedSettingText(await candidate.innerText().catch(() => '')))) continue;
      if (control) return false;
      control = candidate;
    }
    return true;
  };
  if (selectors.length) {
    if (!await inspect(page.locator(selectors.join(',')))) return { control: null, ambiguous: true };
  } else {
    for (const name of uniqueNames) {
      if (!await inspect(page.getByText(name, { exact: true }))) return { control: null, ambiguous: true };
    }
  }
  return { control, ambiguous: false };
}

async function emptyTextSettingReadbackState(root, definition) {
  const readback = definition?.emptyReadback;
  const values = [...new Set([...(readback?.values || []), ...(readback?.names || [])])];
  if (!values.length || (!readback?.selectors?.length && !readback?.names?.length)) return 'missing';
  const match = await uniqueVisibleExactText(root, values, readback.selectors || []);
  if (match.ambiguous) return 'mismatch';
  if (match.control) return 'verified';
  if (!readback.selectors?.length) return 'missing';
  const visible = await uniqueVisible(root, readback.selectors);
  return visible.ambiguous || visible.control ? 'mismatch' : 'missing';
}

async function emptyTextSettingReadback(root, definition) {
  return await emptyTextSettingReadbackState(root, definition) === 'verified';
}

async function booleanTextSettingReadbackState(root, definition, expected) {
  const readback = definition?.booleanReadback;
  const values = expected ? readback?.trueValues : readback?.falseValues;
  if (!values?.length || !readback?.selectors?.length) return 'missing';
  const match = await uniqueVisibleExactText(root, values, readback.selectors);
  if (match.ambiguous) return 'mismatch';
  if (match.control) return 'verified';
  const visible = await uniqueVisible(root, readback.selectors);
  return visible.ambiguous || visible.control ? 'mismatch' : 'missing';
}

async function applyEnumSetting(page, definition, value) {
  if (value === 'none' && definition.emptyReadback) {
    const readback = await emptyTextSettingReadbackState(page, definition);
    if (readback === 'verified') return 'verified';
    if (readback === 'mismatch') return 'unverified';
  }
  const option = definition.options?.[value];
  // "none" is an explicit absence sentinel rather than a browser default.
  // Every concrete value (including public) must be read from the page.
  if (!option) {
    if (!definition.noOpValues?.includes(value) || value !== 'none') return 'unverified';
    return await firstVisibleSettingTrigger(page, definition) ? 'unverified' : 'not_present';
  }
  let textState = await enumSelectedTextState(page, option);
  if (textState.ambiguous) return 'unverified';
  if (textState.selected) return 'verified';
  const stateMatch = await uniqueStateControl(page, option.checkedSelectors);
  if (stateMatch.ambiguous) return 'unverified';
  if (stateMatch.control && await choiceControlState(stateMatch.control) === true) return 'verified';
  let match = await uniqueVisible(page, option.selectors || []);
  if (match.ambiguous) return 'unverified';
  let control = match.control;
  let triggerOpened = false;
  if (!control) {
    match = await uniqueVisibleNamedControl(page, option.names, ['radio', 'checkbox', 'option', 'button']);
    if (match.ambiguous) return 'unverified';
    control = match.control;
  }
  if (!control && definition.triggerNames?.length) {
    const trigger = await uniqueVisibleSettingTrigger(page, definition);
    if (trigger.ambiguous || !trigger.control) return 'unverified';
    if (!await trigger.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) return 'unverified';
    triggerOpened = true;
    match = await uniqueVisible(page, option.selectors || []);
    if (match.ambiguous) return 'unverified';
    control = match.control;
    if (!control) {
      match = await uniqueVisibleNamedControl(page, option.names, ['radio', 'checkbox', 'option', 'button']);
      if (match.ambiguous) return 'unverified';
      control = match.control;
    }
    if (!control) {
      match = await uniqueVisibleExactText(page, option.names, option.choiceSelectors);
      if (match.ambiguous) return 'unverified';
      control = match.control;
    }
  }
  if (!control) return 'unverified';
  const before = stateMatch.control
    ? await choiceControlState(stateMatch.control)
    : await choiceControlState(control);
  if (before === true) {
    if (triggerOpened && page.keyboard?.press) await page.keyboard.press('Escape').catch(() => {});
    return option.confirmNames?.length
      ? (await confirmEnumSelection(page, option) ? 'applied' : 'unverified')
      : 'verified';
  }
  const role = await control.getAttribute('role').catch(() => '');
  const tag = await control.evaluate((node) => node.tagName.toLowerCase()).catch(() => '');
  const type = (await control.getAttribute('type').catch(() => '') || '').toLowerCase();
  if (role === 'radio' || role === 'checkbox' || ['radio', 'checkbox'].includes(type)) {
    if (!await setBooleanControl(control, true)) return 'unverified';
    return await confirmEnumSelection(page, option) ? 'applied' : 'unverified';
  }
  if (!await control.click({ timeout: 1500 }).then(() => true).catch(() => false)) return 'unverified';
  const controlSelected = stateMatch.control
    ? await choiceControlState(stateMatch.control) === true
    : Boolean(tag) && await choiceControlState(control) === true;
  if (option.confirmNames?.length) {
    return await confirmEnumSelection(page, option) ? 'applied' : 'unverified';
  }
  if (controlSelected) return 'applied';
  textState = await enumSelectedTextStateEventually(page, option);
  return textState.selected && !textState.ambiguous ? 'applied' : 'unverified';
}

async function applySettings(page, input, rule) {
  const requested = Object.keys(input.settings);
  if (!requested.length) return {
    applied: [], verified: [], skipped: [], notPresent: [], unsupported: [], unverified: [],
  };
  if (!rule.settingControls) {
    return {
      applied: [], verified: [], skipped: [], notPresent: [], unsupported: requested,
      unverified: requested.map((key) => `settings.${key}`),
    };
  }
  const applied = [];
  const verified = [];
  const skipped = [];
  const notPresent = [];
  const unsupported = [];
  const unverified = [];
  for (const [key, value] of Object.entries(input.settings)) {
    const definition = rule.settingControls[key];
    let status = 'unverified';
    if (!definition) {
      unsupported.push(key);
      unverified.push(`settings.${key}`);
      continue;
    }
    const parentModeDisabled = definition.parentSetting
      && input.settings[definition.parentSetting.key] !== definition.parentSetting.enabledValue
      && (
        definition.parentSetting.absentWhenDisabled === true
        || (typeof value === 'string' && !value.trim())
      );
    if (parentModeDisabled) {
      status = 'not_present';
    } else if (definition?.type === 'text') {
      status = definition.noOpValues?.includes(value) ? 'skipped' : await applyTextSetting(page, definition, value);
    } else if (definition?.type === 'enum') {
      status = await applyEnumSetting(page, definition, value);
      if (status === 'unverified' && await verifiedAbsentEnumSetting(page, rule, definition, value, input.content?.type)) status = 'not_present';
    } else if (definition?.type === 'boolean') {
      const textReadback = await booleanTextSettingReadbackState(page, definition, value);
      if (textReadback === 'verified') {
        verified.push(key);
        continue;
      }
      if (textReadback === 'mismatch') {
        unverified.push(`settings.${key}`);
        continue;
      }
      let match = definition.scopedLabel
        ? await uniqueScopedBooleanControl(page, definition)
        : definition.nearbyLabelNames
          ? await uniqueNearbyBooleanControl(page, definition)
        : await uniqueVisible(page, definition.selectors || []);
      if (match.ambiguous && !definition.checkedSelectors?.length) {
        unverified.push(`settings.${key}`);
        continue;
      }
      let control = match.ambiguous ? null : match.control;
      const clickControl = match.ambiguous ? null : (match.clickControl || null);
      let reviewedClickControl = clickControl;
      if (definition.checkedSelectors?.length
        && (!control || await booleanControlState(control) === null)) {
        const stateMatch = await uniqueStateControl(page, definition.checkedSelectors);
        if (stateMatch.ambiguous) {
          unverified.push(`settings.${key}`);
          continue;
        }
        if (stateMatch.control) {
          control = stateMatch.control;
          reviewedClickControl = await firstVisible(page, definition.selectors || []);
        } else if (match.ambiguous) {
          unverified.push(`settings.${key}`);
          continue;
        }
      }
      if (!control && !definition.scopedLabel) {
        match = await uniqueVisibleNamedControl(page, definition.names, ['switch', 'checkbox']);
        if (match.ambiguous) {
          unverified.push(`settings.${key}`);
          continue;
        }
        control = match.control;
      }
      if (!control && definition.checkedSelectors?.length) {
        const stateMatch = await uniqueStateControl(page, definition.checkedSelectors);
        if (stateMatch.ambiguous) {
          unverified.push(`settings.${key}`);
          continue;
        }
        control = stateMatch.control;
        reviewedClickControl = await firstVisible(page, definition.selectors || []);
      }
      const current = control ? await booleanControlState(control) : null;
      if (current === value) status = 'verified';
      else if (!control && await verifiedAbsentBooleanSetting(page, rule, definition, value, input.content?.type)) status = 'not_present';
      else status = control && await setBooleanControl(control, value, reviewedClickControl) ? 'applied' : 'unverified';
    }
    if (status === 'applied') applied.push(key);
    else if (status === 'verified') verified.push(key);
    else if (status === 'skipped') skipped.push(key);
    else if (status === 'not_present') notPresent.push(key);
    else unverified.push(`settings.${key}`);
  }
  const locationMode = unverified.indexOf('settings.extension_type');
  if (input.settings.extension_type === 'location' && applied.includes('location') && locationMode >= 0) {
    // The selected official POI is stronger evidence than the transient
    // extension menu. A resumed draft may already have that menu collapsed.
    unverified.splice(locationMode, 1);
    applied.push('extension_type');
  }
  return { applied, verified, skipped, notPresent, unsupported, unverified };
}

function localPublishTimeParts(raw) {
  const value = new Date(raw);
  const two = (number) => String(number).padStart(2, '0');
  const date = `${value.getFullYear()}-${two(value.getMonth() + 1)}-${two(value.getDate())}`;
  const time = `${two(value.getHours())}:${two(value.getMinutes())}`;
  return {
    date,
    time,
    dateTime: `${date} ${time}:00`,
    dateTimeMinute: `${date} ${time}`,
    dateTimeLocal: `${date}T${time}`,
  };
}

async function scheduleChoiceState(control) {
  const checked = await control.isChecked?.().catch(() => null);
  if (checked === true || checked === false) return checked;
  const ariaChecked = await control.getAttribute?.('aria-checked').catch(() => null);
  if (ariaChecked === 'true' || ariaChecked === 'false') return ariaChecked === 'true';
  return null;
}

async function exactScheduleChoice(page, definition) {
  const selectedStateVisible = async () => {
    const selected = await uniqueVisible(page, definition.selectedStateSelectors || []);
    return !selected.ambiguous && Boolean(selected.control);
  };
  if (await selectedStateVisible()) return true;
  if (definition.scopedLabel) {
    const scoped = await uniqueScopedBooleanControl(page, definition);
    if (scoped.ambiguous || !scoped.control) return false;
    if (await scheduleChoiceState(scoped.control) !== true) {
      if (!scoped.clickControl
        || !await scoped.clickControl.click({ timeout: 1500 }).then(() => true).catch(() => false)) return false;
      await page.waitForTimeout(100);
    }
    return await scheduleChoiceState(scoped.control) === true;
  }
  if (definition.nearbyLabelNames?.length && definition.nearbyStateSelector) {
    const nearby = await uniqueNearbyBooleanControl(page, definition);
    if (nearby.ambiguous || !nearby.control) return false;
    const before = await scheduleChoiceState(nearby.control);
    if (before !== true) {
      await (nearby.clickControl || nearby.control).click({ timeout: 1500 }).catch(() => {});
      await page.waitForTimeout(100);
      if (await scheduleChoiceState(nearby.control) !== true) {
        // WeChat renders the text label separately from the visual switch.
        // Use only the exact checkbox's own label as the bounded fallback.
        const switchLabel = nearby.control.locator('xpath=ancestor::label[1]');
        if (await switchLabel.count() === 1) {
          await switchLabel.click({ force: true, timeout: 1500 }).catch(() => {});
          await page.waitForTimeout(100);
        }
      }
      if (await scheduleChoiceState(nearby.control) !== true && definition.confirmNames?.length) {
        const confirm = await uniqueVisibleNamedControl(page, definition.confirmNames, ['button']);
        if (confirm.ambiguous || !confirm.control
          || !await confirm.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) return false;
        await page.waitForTimeout(100);
      }
      if (await scheduleChoiceState(nearby.control) !== true
        && !await setBooleanControl(nearby.control, true)) {
        return false;
      }
    }
    return !definition.requireCheckedChoice || await scheduleChoiceState(nearby.control) === true;
  }
  for (const name of definition.toggleNames || []) {
    for (const role of ['radio', 'checkbox', 'button']) {
      const candidate = page.getByRole(role, { name, exact: true }).first();
      if (!await candidate.count() || !await candidate.isVisible().catch(() => false)) continue;
      const before = await scheduleChoiceState(candidate);
      if (before !== true
        && !await candidate.click({ timeout: 1500 }).then(() => true).catch(() => false)) return false;
      const after = await scheduleChoiceState(candidate);
      return definition.requireCheckedChoice ? after === true || await selectedStateVisible() : true;
    }
    const text = page.getByText(name, { exact: true }).first();
    if (await text.count() && await text.isVisible().catch(() => false)) {
      if (!await text.click({ timeout: 1500 }).then(() => true).catch(() => false)) return false;
      await page.waitForTimeout(100);
      return !definition.requireCheckedChoice || await selectedStateVisible();
    }
  }
  return false;
}

async function exactScheduleChoiceChecked(page, definition) {
  const selected = await uniqueVisible(page, definition.selectedStateSelectors || []);
  if (!selected.ambiguous && selected.control) return true;
  if (definition.scopedLabel) {
    const scoped = await uniqueScopedBooleanControl(page, definition);
    return !scoped.ambiguous && Boolean(scoped.control)
      && await scheduleChoiceState(scoped.control) === true;
  }
  if (definition.nearbyLabelNames?.length && definition.nearbyStateSelector) {
    const nearby = await uniqueNearbyBooleanControl(page, definition);
    return !nearby.ambiguous && Boolean(nearby.control)
      && await scheduleChoiceState(nearby.control) === true;
  }
  for (const name of definition.toggleNames || []) {
    for (const role of ['radio', 'checkbox']) {
      const matches = page.getByRole(role, { name, exact: true });
      if (await matches.count() !== 1) continue;
      const control = matches.first();
      if (!await control.isVisible().catch(() => false)) continue;
      return await scheduleChoiceState(control) === true;
    }
  }
  return false;
}

function scheduleMaximumValue(raw, type, definition) {
  const value = String(raw || '').trim();
  if (!value) return '';
  if (type === 'datetime-local') {
    return /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2})?$/.test(value) ? value.slice(0, 16) : '';
  }
  if (definition.dateTimeFormat === 'yyyy-MM-dd HH:mm') {
    return /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/.test(value) ? value : '';
  }
  return /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}(?::\d{2})?$/.test(value)
    ? (value.length === 16 ? `${value}:00` : value)
    : '';
}

function scheduleDateMaximumValue(raw) {
  const value = String(raw || '').trim();
  return /^\d{4}-\d{2}-\d{2}$/.test(value) ? value : '';
}

function scheduleTimeMaximumValue(raw) {
  const value = String(raw || '').trim();
  return /^\d{2}:\d{2}(?::\d{2})?$/.test(value) ? value.slice(0, 5) : '';
}

async function scheduleMaximumAttribute(control, attributes) {
  for (const attribute of attributes || ['max']) {
    const value = await control.getAttribute(attribute).catch(() => '');
    if (String(value || '').trim()) return value;
  }
  return '';
}

async function scheduleInputMatches(control, expected) {
  return await control.inputValue().catch(() => null) === expected;
}

async function dispatchNativeInputValue(control, value) {
  const dispatched = await control.evaluate((node, nextValue) => {
    const prototype = node instanceof HTMLInputElement
      ? HTMLInputElement.prototype
      : node instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : null;
    const setter = prototype && Object.getOwnPropertyDescriptor(prototype, 'value')?.set;
    if (!setter) return false;
    setter.call(node, nextValue);
    node.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: nextValue }));
    node.dispatchEvent(new Event('change', { bubbles: true }));
    return node.value === nextValue;
  }, value).catch(() => false);
  if (!dispatched) return false;
  await control.page().waitForTimeout(100);
  return scheduleInputMatches(control, value);
}

async function fillScheduleInput(control, value, definition) {
  if (await fillLocator(control, value)) return true;
  if (definition.nativeTyping && await dispatchNativeInputValue(control, value)) return true;
  if (!definition.nativeTyping) return false;
  try {
    await control.click({ timeout: 1500 });
    await control.press(process.platform === 'darwin' ? 'Meta+A' : 'Control+A');
    await control.type(value, { delay: 20 });
    await control.press('Enter');
    return await scheduleInputMatches(control, value);
  } catch (_) {
    return false;
  }
}

async function chooseWechatVideoDateTimePicker(page, control, parts) {
  try {
    const current = await control.inputValue();
    if (!await control.click({ timeout: 1500 }).then(() => true).catch(() => false)) return false;
    let picker = await firstVisibleEventually(
      page,
      ['.weui-desktop-picker__date-time', '.calendar-wrapper'],
      1500,
    );
    if (!picker) return false;
    if (!current.startsWith(`${parts.date} `)) {
      const [targetYear, targetMonth, targetDay] = parts.date.split('-').map(Number);
      let displayed = null;
      for (let attempt = 0; attempt < 3; attempt += 1) {
        const labels = picker.locator('.weui-desktop-picker__panel__label');
        if (await labels.count() < 2) return false;
        const year = Number((await labels.nth(0).innerText()).replace(/\D/g, ''));
        const month = Number((await labels.nth(1).innerText()).replace(/\D/g, ''));
        if (!year || !month) return false;
        displayed = (year * 12) + month;
        const target = (targetYear * 12) + targetMonth;
        if (displayed === target) break;
        const direction = displayed < target
          ? '.weui-desktop-btn__icon__right'
          : '.weui-desktop-btn__icon__left';
        const next = picker.locator(direction);
        if (await next.count() !== 1
          || !await next.click({ timeout: 1500 }).then(() => true).catch(() => false)) return false;
        await page.waitForTimeout(100);
      }
      if (displayed !== (targetYear * 12) + targetMonth) return false;
      const days = picker.locator(
        '.weui-desktop-picker__table a:not(.weui-desktop-picker__disabled):not(.weui-desktop-picker__faded)',
      );
      let day = null;
      for (let index = 0; index < await days.count(); index += 1) {
        const candidate = days.nth(index);
        if (Number((await candidate.innerText()).trim()) !== targetDay) continue;
        if (day) return false;
        day = candidate;
      }
      if (!day || !await day.click({ timeout: 1500 }).then(() => true).catch(() => false)) return false;
      await page.waitForTimeout(100);
      if (!(await control.inputValue()).startsWith(`${parts.date} `)) return false;
      if (!await picker.isVisible().catch(() => false)) {
        await control.click({ timeout: 1500 }).catch(() => {});
        picker = await firstVisibleEventually(
          page,
          ['.weui-desktop-picker__date-time', '.calendar-wrapper'],
          1500,
        );
        if (!picker) return false;
      }
    }
    const inputs = picker.locator('input');
    let timeInput = null;
    for (let index = 0; index < await inputs.count(); index += 1) {
      const candidate = inputs.nth(index);
      if (!await candidate.isVisible().catch(() => false)) continue;
      if (!/^\d{2}:\d{2}$/.test(await candidate.inputValue().catch(() => ''))) continue;
      if (timeInput) return false;
      timeInput = candidate;
    }
    if (!timeInput) return false;
    await timeInput.click({ timeout: 1500 }).catch(() => {});
    const officialTimePicker = await firstVisibleEventually(
      page,
      ['.weui-desktop-picker__time'],
      1500,
    );
    if (!officialTimePicker) return false;
    if (await officialTimePicker.count() === 1) {
      const pickPart = async (columnSelector, value) => {
        const column = officialTimePicker.locator(columnSelector);
        if (await column.count() !== 1) return false;
        const wanted = new Set([value, String(Number(value))]);
        const items = column.locator('li');
        let choice = null;
        for (let index = 0; index < await items.count(); index += 1) {
          const item = items.nth(index);
          if (!wanted.has((await item.innerText().catch(() => '')).trim())) continue;
          if (choice) return false;
          choice = item;
        }
        if (!choice) return false;
        await choice.scrollIntoViewIfNeeded().catch(() => {});
        return choice.click({ timeout: 1500 }).then(() => true).catch(() => false);
      };
      const [hour, minute] = parts.time.split(':');
      if (await pickPart('.weui-desktop-picker__time__hour', hour)
        && await pickPart('.weui-desktop-picker__time__minute', minute)) {
        await page.waitForTimeout(100);
        if (await scheduleInputMatches(control, parts.dateTimeMinute)) {
          await timeInput.press('Escape').catch(() => {});
          return true;
        }
      }
    }
    if (!await fillLocator(timeInput, parts.time)) {
      const dispatched = await dispatchNativeInputValue(timeInput, parts.time);
      if (!dispatched) {
        await timeInput.click({ timeout: 1500 });
        await timeInput.press(process.platform === 'darwin' ? 'Meta+A' : 'Control+A');
        await timeInput.type(parts.time, { delay: 20 });
      }
    }
    await timeInput.press('Enter').catch(() => {});
    await timeInput.press('Tab').catch(() => {});
    await page.waitForTimeout(100);
    return await scheduleInputMatches(control, parts.dateTimeMinute);
  } catch (_) {
    return false;
  }
}

async function chooseWechatMPDateTimePicker(page, parts) {
  try {
    const rootMatch = await uniqueVisible(page, ['.new_mass_send_dialog .weui-desktop-dialog']);
    if (rootMatch.ambiguous || !rootMatch.control) return null;
    const root = rootMatch.control;
    const dateTrigger = root.locator(
      '.mass-send__timer-container .weui-desktop-form__dropdown__dt',
    );
    if (await dateTrigger.count() !== 1 || !await dateTrigger.isVisible().catch(() => false)) return null;

    const today = localPublishTimeParts(new Date().toISOString());
    const tomorrowDate = new Date(`${today.date}T12:00:00`);
    tomorrowDate.setDate(tomorrowDate.getDate() + 1);
    const tomorrow = localPublishTimeParts(tomorrowDate.toISOString());
    const [targetYear, targetMonth, targetDay] = parts.date.split('-').map(Number);
    const dateLabel = parts.date === today.date
      ? '今天'
      : parts.date === tomorrow.date
        ? '明天'
        : `${targetMonth}月${targetDay}日`;
    const selectedDate = dateTrigger.locator('.weui-desktop-form__dropdown__value');
    if (await selectedDate.count() !== 1) return null;
    if (normalizedSettingText(await selectedDate.innerText().catch(() => '')) !== normalizedSettingText(dateLabel)) {
      if (!await dateTrigger.click({ timeout: 1500 }).then(() => true).catch(() => false)) return null;
      const option = await uniqueVisibleExactText(
        root,
        [dateLabel],
        ['.mass-send__timer-container .weui-desktop-dropdown__list-ele__text'],
      );
      if (option.ambiguous || !option.control
        || !await option.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) return null;
      await page.waitForTimeout(100);
    }
    if (normalizedSettingText(await selectedDate.innerText().catch(() => '')) !== normalizedSettingText(dateLabel)) return null;

    const pickers = root.locator('.mass-send__timer-container .weui-desktop-picker__time');
    let picker = null;
    for (let index = 0; index < await pickers.count(); index += 1) {
      const candidate = pickers.nth(index);
      if (!await candidate.isVisible().catch(() => false)) continue;
      if (picker) return null;
      picker = candidate;
    }
    if (!picker) return null;
    const timeInput = picker.locator("input[placeholder='请选择时间']");
    if (await timeInput.count() !== 1) return null;
    if (!await scheduleInputMatches(timeInput, parts.time)) {
      if (!await timeInput.click({ timeout: 1500 }).then(() => true).catch(() => false)) return null;
      const panel = picker.locator('.weui-desktop-picker__dd__time');
      if (await panel.count() !== 1 || !await panel.isVisible().catch(() => false)) return null;
      const pickPart = async (selector, value) => {
        const items = panel.locator(`${selector} li:not(.weui-desktop-picker__disabled)`);
        let choice = null;
        for (let index = 0; index < await items.count(); index += 1) {
          const candidate = items.nth(index);
          if ((await candidate.innerText().catch(() => '')).trim() !== value) continue;
          if (choice) return false;
          choice = candidate;
        }
        if (!choice) return false;
        await choice.scrollIntoViewIfNeeded().catch(() => {});
        return choice.click({ timeout: 1500 }).then(() => true).catch(() => false);
      };
      const [hour, minute] = parts.time.split(':');
      if (!await pickPart('.weui-desktop-picker__time__hour', hour)
        || !await pickPart('.weui-desktop-picker__time__minute', minute)) return null;
      await timeInput.press('Escape').catch(() => {});
      await page.waitForTimeout(100);
    }
    if (!await scheduleInputMatches(timeInput, parts.time)) return null;
    return {
      value: parts.dateTime,
      requestedValue: parts.dateTime,
      fallbackUsed: false,
    };
  } catch (_) {
    return null;
  }
}

async function chooseAntDateTimePicker(page, control, parts) {
  try {
    await control.click({ timeout: 1500 });
    const picker = page.locator('.ant-picker-dropdown:visible').last();
    const day = picker.locator(`.ant-picker-cell[title='${parts.date}']:not(.ant-picker-cell-disabled)`);
    if (await day.count() !== 1) return false;
    await day.click({ timeout: 1500 });
    const columns = picker.locator('.ant-picker-time-panel-column');
    if (await columns.count() < 2) return false;
    const values = [parts.hour, parts.minute, parts.second];
    for (let index = 0; index < values.length && index < await columns.count(); index += 1) {
      const option = columns.nth(index).getByText(String(Number(values[index])), { exact: true });
      if (await option.count() !== 1) return false;
      await option.click({ timeout: 1500 });
    }
    const confirm = picker.getByRole('button', { name: '确定', exact: true });
    if (await confirm.count() !== 1) return false;
    await confirm.click({ timeout: 1500 });
    return await scheduleInputMatches(control, parts.dateTime);
  } catch (_) {
    return false;
  }
}

function scheduleValueForControl(parts, type, definition) {
  if (type === 'datetime-local') return parts.dateTimeLocal;
  return definition.dateTimeFormat === 'yyyy-MM-dd HH:mm' ? parts.dateTimeMinute : parts.dateTime;
}

function scheduleDisplayValue(value, definition) {
  if (!value) return '';
  if (definition.dateTimeFormat === 'yyyy-MM-dd HH:mm') return value;
  return value.replace('T', ' ').length === 16 ? `${value.replace('T', ' ')}:00` : value.replace('T', ' ');
}

function schedulePartsAtMinuteStep(parts, minuteStep) {
  if (!Number.isInteger(minuteStep) || minuteStep <= 0 || minuteStep >= 60) return parts;
  const [hourText, minuteText] = parts.time.split(':');
  const total = Number(hourText) * 60 + Number(minuteText);
  const rounded = Math.ceil(total / minuteStep) * minuteStep;
  if (rounded < 24 * 60) {
    return { ...parts, time: `${String(Math.floor(rounded / 60)).padStart(2, '0')}:${String(rounded % 60).padStart(2, '0')}` };
  }
  const next = new Date(`${parts.date}T00:00:00`);
  next.setDate(next.getDate() + 1);
  const two = (value) => String(value).padStart(2, '0');
  return { ...parts, date: `${next.getFullYear()}-${two(next.getMonth() + 1)}-${two(next.getDate())}`, time: '00:00' };
}

function effectiveNativePublishAt(platform, raw) {
  const value = new Date(raw);
  if (Number.isNaN(value.getTime())) return '';
  const schedule = PLATFORM_RULES[platform]?.scheduleControl;
  const minuteStep = schedule?.tiktokDateTimePicker?.minuteStep
    || schedule?.youtubeDateTimePicker?.minuteStep
    || schedule?.minuteStep
    || 0;
  value.setSeconds(0, 0);
  if (Number.isInteger(minuteStep) && minuteStep > 0 && minuteStep < 60) {
    value.setMinutes(Math.ceil(value.getMinutes() / minuteStep) * minuteStep);
  }
  return value.toISOString();
}

async function chooseElementDateTimePicker(page, definition, parts) {
  const dateInput = await firstVisibleEventually(page, [definition.dateSelector], 3000);
  const timeInput = await firstVisibleEventually(page, [definition.timeSelector], 3000);
  if (!dateInput || !timeInput) return null;
  const actual = schedulePartsAtMinuteStep(parts, definition.minuteStep);
  if (!await dateInput.click({ timeout: 1500 }).then(() => true).catch(() => false)) return null;
  const calendar = page.locator('.el-picker-panel:visible').last();
  const cells = calendar.locator('td.available:not(.prev-month):not(.next-month):not(.disabled)');
  let day = null;
  const wantedDay = String(Number(actual.date.slice(-2)));
  for (let index = 0; index < await cells.count(); index += 1) {
    const candidate = cells.nth(index);
    if ((await candidate.innerText().catch(() => '')).trim() !== wantedDay) continue;
    day = candidate;
    break;
  }
  if (day) await day.click({ timeout: 1500 }).catch(() => {});
  const readyDeadline = Date.now() + 3000;
  while (await timeInput.isDisabled().catch(() => true) && Date.now() < readyDeadline) {
    await page.waitForTimeout(100);
  }
  if (await timeInput.isDisabled().catch(() => true)) {
    // Element's date input sometimes displays the selected day before its
    // Vue `change` handler commits. Confirm the same reviewed input rather
    // than touching an unrelated calendar control.
    await dateInput.fill(actual.date, { timeout: 1500 }).catch(() => {});
    await dateInput.press('Enter', { timeout: 1500 }).catch(() => {});
    await dateInput.press('Tab', { timeout: 1500 }).catch(() => {});
    const retryDeadline = Date.now() + 1500;
    while (await timeInput.isDisabled().catch(() => true) && Date.now() < retryDeadline) {
      await page.waitForTimeout(100);
    }
  }
  if (await timeInput.isDisabled().catch(() => true)) return null;
  if (!await timeInput.click({ timeout: 1500 }).then(() => true).catch(() => false)) return null;
  const option = await uniqueVisibleExactText(
    page,
    [actual.time],
    ['.el-picker-panel.time-select .time-select-item', '.el-time-select .time-select-item'],
  );
  if (option.ambiguous || !option.control
    || !await option.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) return null;
  await page.waitForTimeout(100);
  if (!await scheduleInputMatches(dateInput, actual.date) || !await scheduleInputMatches(timeInput, actual.time)) return null;
  return {
    value: `${actual.date} ${actual.time}:00`,
    requestedValue: parts.dateTime,
    fallbackUsed: actual.date !== parts.date || actual.time !== parts.time,
  };
}

function youtubeScheduleDateLabels(parts) {
  const [year, month, day] = parts.date.split('-').map(Number);
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  if (!year || !month || !day || !months[month - 1]) return [];
  return [
    `${months[month - 1]} ${day}, ${year}`,
    `${year}年${month}月${day}日`,
  ];
}

function youtubeScheduleTimeLabel(parts) {
  const [hour, minute] = parts.time.split(':').map(Number);
  if (!Number.isInteger(hour) || !Number.isInteger(minute)) return '';
  const period = hour >= 12 ? 'PM' : 'AM';
  const twelveHour = hour % 12 || 12;
  return `${twelveHour}:${String(minute).padStart(2, '0')}\u202f${period}`;
}

async function youtubeScheduleTimeMatches(control, expected) {
  const actual = await control.inputValue().catch(() => null);
  return actual !== null && normalizedSettingText(actual) === normalizedSettingText(expected);
}

async function dismissYoutubeTimePicker(page, timeControl, expectedTime) {
  const pickers = page.locator('tp-yt-paper-dialog.ytcp-time-of-day-picker');
  let visibleCount = 0;
  for (let index = 0; index < await pickers.count(); index += 1) {
    if (await pickers.nth(index).isVisible().catch(() => false)) visibleCount += 1;
  }
  if (visibleCount > 1) return false;
  if (visibleCount === 1) {
    await page.keyboard.press('Escape').catch(() => {});
    await page.waitForTimeout(100);
    for (let index = 0; index < await pickers.count(); index += 1) {
      if (await pickers.nth(index).isVisible().catch(() => false)) return false;
    }
  }
  return youtubeScheduleTimeMatches(timeControl, expectedTime);
}

async function youtubeAutosaveVisible(page) {
  const labels = [/^\s*Saving/i, /^\s*正在保存/];
  for (const label of labels) {
    const matches = page.getByText(label);
    const count = Math.min(await matches.count(), 10);
    for (let index = 0; index < count; index += 1) {
      if (await matches.nth(index).isVisible().catch(() => false)) {
        return true;
      }
    }
  }
  return false;
}

async function chooseYoutubeDateTimePicker(page, definition, parts) {
  const deadline = Date.now() + (definition.autosaveTimeoutMs || 15000);
  if (definition.settleMs) await page.waitForTimeout(definition.settleMs);
  const actual = schedulePartsAtMinuteStep(parts, definition.minuteStep);
  const expectedDates = youtubeScheduleDateLabels(actual).map(normalizedSettingText);
  const expectedTime = youtubeScheduleTimeLabel(actual);
  if (!expectedTime) return null;
  const attempts = [
    { value: expectedTime.replace('\u202f', ' '), commit: 'Tab', insert: true },
    { value: expectedTime.replace('\u202f', ' '), commit: 'Enter', insert: true },
    { value: expectedTime.replace('\u202f', ' '), commit: 'Tab' },
    { value: expectedTime, commit: 'Tab' },
    { value: expectedTime.replace('\u202f', ''), commit: 'Tab' },
    { value: expectedTime.replace('\u202f', ' '), commit: 'Enter' },
  ];
  retryYoutubeTime:
  while (Date.now() < deadline) {
    // The Saving badge is inside YouTube's shadow DOM on some builds. Treat it
    // only as a passive hint; the time field's stable readback is authoritative.
    if (await youtubeAutosaveVisible(page)) {
      await page.waitForTimeout(500);
      continue;
    }
    const dateMatch = await uniqueVisible(page, definition.dateTriggerSelectors || []);
    const timeMatch = await uniqueVisible(page, definition.timeInputSelectors || []);
    if (dateMatch.ambiguous || timeMatch.ambiguous || !dateMatch.control || !timeMatch.control) {
      await page.waitForTimeout(500);
      continue;
    }
    const actualDate = normalizedSettingText(await dateMatch.control.innerText().catch(() => ''));
    if (!expectedDates.includes(actualDate)) {
      await page.waitForTimeout(500);
      continue;
    }
    for (const attempt of attempts) {
      if (await youtubeScheduleTimeMatches(timeMatch.control, expectedTime)) break;
      if (await timeMatch.control.isDisabled().catch(() => true)
        || !await timeMatch.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) {
        await page.waitForTimeout(500);
        continue retryYoutubeTime;
      }
      // Fingerprint Chromium can advertise a different platform than the host
      // Node process. Select the reviewed input directly so replacement does
      // not depend on whether the page expects Control or Command.
      await timeMatch.control.selectText({ timeout: 1500 }).catch(() => {});
      if (attempt.insert) {
        await timeMatch.control.page().keyboard.insertText(attempt.value).catch(() => {});
      } else {
        await timeMatch.control.pressSequentially(attempt.value, { delay: 20 }).catch(() => {});
      }
      await timeMatch.control.press(attempt.commit).catch(() => {});
      await page.waitForTimeout(200);
    }
    if (await youtubeScheduleTimeMatches(timeMatch.control, expectedTime)) {
      await timeMatch.control.press('Tab').catch(() => {});
      await page.waitForTimeout(1500);
      if (await youtubeScheduleTimeMatches(timeMatch.control, expectedTime)
        && await dismissYoutubeTimePicker(page, timeMatch.control, expectedTime)) {
        return {
          value: `${actual.date} ${actual.time}:00`,
          requestedValue: parts.dateTime,
          fallbackUsed: actual.date !== parts.date || actual.time !== parts.time,
        };
      }
    }
    await page.waitForTimeout(750);
  }
  return null;
}

async function chooseTikTokDateTimePicker(page, definition, parts) {
  const inputs = page.locator(definition.inputSelector);
  if (await inputs.count() !== 2) return null;
  const visible = [];
  for (let index = 0; index < 2; index += 1) {
    const input = inputs.nth(index);
    if (await input.isVisible().catch(() => false)) visible.push(input);
  }
  if (visible.length !== 2) return null;
  let dateInput = null;
  let timeInput = null;
  for (const input of visible) {
    const value = await input.inputValue().catch(() => '');
    if (/^\d{4}-\d{2}-\d{2}$/.test(value)) dateInput = input;
    else if (/^\d{2}:\d{2}$/.test(value)) timeInput = input;
  }
  if (!dateInput || !timeInput) return null;
  const actual = schedulePartsAtMinuteStep(parts, definition.minuteStep);
  const currentDate = await dateInput.inputValue();
  if (currentDate !== actual.date) {
    const base = new Date(`${currentDate}T12:00:00`);
    const target = new Date(`${actual.date}T12:00:00`);
    if (Number.isNaN(base.getTime()) || Number.isNaN(target.getTime())) return null;
    const monthDelta = (target.getFullYear() - base.getFullYear()) * 12
      + target.getMonth() - base.getMonth();
    if (monthDelta < 0 || monthDelta > 1) return null;
    if (!await dateInput.click({ timeout: 1500 }).then(() => true).catch(() => false)) return null;
    const calendar = await firstVisibleEventually(page, [definition.calendarSelector], 1500);
    if (!calendar) return null;
    const leadingDays = new Date(base.getFullYear(), base.getMonth(), 1).getDay();
    const daysInBaseMonth = new Date(base.getFullYear(), base.getMonth() + 1, 0).getDate();
    const cellIndex = leadingDays + (monthDelta === 0
      ? target.getDate() - 1
      : daysInBaseMonth + target.getDate() - 1);
    const cells = page.locator(definition.daySelector);
    if (cellIndex < 0 || cellIndex >= await cells.count()
      || !await cells.nth(cellIndex).click({ timeout: 1500 }).then(() => true).catch(() => false)) return null;
    if (!await scheduleInputMatches(dateInput, actual.date)) return null;
  }
  if (await timeInput.inputValue() !== actual.time) {
    if (!await timeInput.click({ timeout: 1500 }).then(() => true).catch(() => false)) return null;
    const [hour, minute] = actual.time.split(':');
    const selectPart = async (value, selector, matches) => {
      let option = await uniqueVisibleExactText(page, [value], [selector]);
      // TikTok's clock columns are virtualized in current Studio builds: only
      // a few values around the default time are mounted. Look through the
      // reviewed scrollable column before treating an absent option as drift.
      if (!option.control && !option.ambiguous) {
        const options = page.locator(selector);
        let sample = null;
        for (let index = 0; index < await options.count(); index += 1) {
          const candidate = options.nth(index);
          if (await candidate.isVisible().catch(() => false)) {
            sample = candidate;
            break;
          }
        }
        let scroller = sample ? sample.locator('xpath=..') : null;
        for (let depth = 0; scroller && depth < 6; depth += 1) {
          const metrics = await scroller.evaluate((node) => ({
            scrollable: (() => {
              const before = node.scrollTop;
              node.scrollTop = Math.min(before + 1, node.scrollHeight);
              const moved = node.scrollTop !== before;
              node.scrollTop = before;
              return moved;
            })(),
          })).catch(() => null);
          if (metrics?.scrollable) break;
          scroller = scroller.locator('xpath=..');
          if (depth === 5) scroller = null;
        }
        if (scroller) {
          await scroller.evaluate((node) => {
            node.scrollTop = 0;
            node.dispatchEvent(new Event('scroll', { bubbles: true }));
          }).catch(() => {});
          for (let attempt = 0; attempt < 32; attempt += 1) {
            await page.waitForTimeout(40);
            option = await uniqueVisibleExactText(page, [value], [selector]);
            if (option.control || option.ambiguous) break;
            const moved = await scroller.evaluate((node) => {
              const before = node.scrollTop;
              node.scrollTop = Math.min(node.scrollTop + Math.max(40, Math.floor(node.clientHeight * 0.75)), node.scrollHeight);
              node.dispatchEvent(new Event('scroll', { bubbles: true }));
              return node.scrollTop > before;
            }).catch(() => false);
            if (!moved) break;
          }
        }
        // Some Studio releases keep the wheel state outside a scrollable DOM
        // ancestor. A bounded wheel scan at the reviewed option column is the
        // last non-submitting fallback for that rendered control.
        if (!option.control && !option.ambiguous && sample) {
          const box = await sample.boundingBox().catch(() => null);
          if (box) {
            await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2).catch(() => {});
            for (let attempt = 0; attempt < 12; attempt += 1) {
              await page.mouse.wheel(0, -320).catch(() => {});
              await page.waitForTimeout(80);
              option = await uniqueVisibleExactText(page, [value], [selector]);
              if (option.control || option.ambiguous) break;
            }
          }
        }
      }
      // The current Studio clock can render a visual wheel without mounting
      // the requested text node. Its focused trigger still supports the
      // native previous-option key. Use that narrow fallback only after the
      // reviewed DOM and wheel scans failed, and accept it solely on exact
      // input readback below.
      if (!option.control && !option.ambiguous) {
        for (let attempt = 0; attempt < 12; attempt += 1) {
          if (!await timeInput.press('ArrowUp', { timeout: 1500 }).then(() => true).catch(() => false)) break;
          await page.waitForTimeout(80);
          if (matches(await timeInput.inputValue().catch(() => ''))) return true;
          option = await uniqueVisibleExactText(page, [value], [selector]);
          if (option.control || option.ambiguous) break;
        }
      }
      if (option.ambiguous || !option.control) return false;
      const parent = option.control.locator('xpath=..');
      await parent.click({ timeout: 1500 }).catch(() => {});
      await page.waitForTimeout(100);
      if (matches(await timeInput.inputValue().catch(() => ''))) return true;
      // TikTok has shipped both variants: the option item is clickable in
      // one build, while only its text span handles the minute selection in
      // another. Re-resolve after the first click because the list can rerender.
      option = await uniqueVisibleExactText(page, [value], [selector]);
      if (!option.control || option.ambiguous) {
        await timeInput.click({ timeout: 1500 }).catch(() => {});
        option = await uniqueVisibleExactText(page, [value], [selector]);
      }
      if (option.ambiguous || !option.control
        || !await option.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) return false;
      await page.waitForTimeout(100);
      return matches(await timeInput.inputValue().catch(() => ''));
    };
    const current = await timeInput.inputValue();
    if (!current.startsWith(`${hour}:`)
      && !await selectPart(hour, definition.hourSelector, (value) => value.startsWith(`${hour}:`))) return null;
    if (!(await timeInput.inputValue()).endsWith(`:${minute}`)
      && !await selectPart(minute, definition.minuteSelector, (value) => value.endsWith(`:${minute}`))) return null;
    if (!await scheduleInputMatches(timeInput, actual.time)) return null;
  }
  // The visible TikTok date/time controls exist only while native scheduling
  // is selected. Exact readback of both stored values is therefore the final
  // state proof; re-resolving the visually hidden radio after the final
  // button also changes to “Schedule/预约发布” creates a false ambiguity.
  return {
    value: `${actual.date} ${actual.time}:00`,
    requestedValue: parts.dateTime,
    fallbackUsed: actual.date !== parts.date || actual.time !== parts.time,
  };
}

async function applyPublishTime(page, input, rule) {
  if (!input.publishAt) return { applied: [], unverified: [], value: '', requestedValue: '', fallbackUsed: false, maximumValue: '' };
  if (!rule.scheduleControl) return { applied: [], unverified: ['publish.time'], value: '', requestedValue: '', fallbackUsed: false, maximumValue: '' };
  if (!await exactScheduleChoice(page, rule.scheduleControl)) {
    return { applied: [], unverified: ['publish.time'], value: '', requestedValue: '', fallbackUsed: false, maximumValue: '' };
  }

  const parts = localPublishTimeParts(input.publishAt);
  if (rule.scheduleControl.wechatMPDateTimePicker) {
    const selected = await chooseWechatMPDateTimePicker(page, parts);
    if (selected) {
      return { applied: ['publish.time'], unverified: [], maximumValue: '', ...selected };
    }
  }
  if (rule.scheduleControl.tiktokDateTimePicker) {
    const selected = await chooseTikTokDateTimePicker(page, rule.scheduleControl.tiktokDateTimePicker, parts);
    if (selected) {
      return { applied: ['publish.time'], unverified: [], maximumValue: '', ...selected };
    }
  }
  if (rule.scheduleControl.elementDateTimePicker) {
    const selected = await chooseElementDateTimePicker(page, rule.scheduleControl.elementDateTimePicker, parts);
    if (selected) {
      return { applied: ['publish.time'], unverified: [], maximumValue: '', ...selected };
    }
  }
  if (rule.scheduleControl.youtubeDateTimePicker) {
    const selected = await chooseYoutubeDateTimePicker(page, rule.scheduleControl.youtubeDateTimePicker, parts);
    if (selected && await exactScheduleChoiceChecked(page, rule.scheduleControl)) {
      return { applied: ['publish.time'], unverified: [], maximumValue: '', ...selected };
    }
  }
  let combined = await firstVisibleEventually(page, rule.scheduleControl.dateTimeInputs || [], 3000);
  if (!combined && rule.scheduleControl.dateTimeNearbyLabelNames?.length) {
    const nearby = await uniqueNearbyValueControl(
      page,
      rule.scheduleControl.dateTimeNearbyLabelNames,
      rule.scheduleControl.dateTimeNearbySelector || 'input',
      rule.scheduleControl.dateTimeNearbyMaxDepth || 5,
    );
    if (!nearby.ambiguous) combined = nearby.control;
  }
  if (combined) {
    const type = (await combined.getAttribute('type').catch(() => '') || '').toLowerCase();
    const requestedValue = scheduleValueForControl(parts, type, rule.scheduleControl);
    const maximumValue = scheduleMaximumValue(
      await scheduleMaximumAttribute(combined, rule.scheduleControl.maximumAttributes),
      type,
      rule.scheduleControl,
    );
    // The native control's max attribute is the only reviewed evidence for a
    // platform-specific longest date. Never guess a far-future date when a
    // custom picker does not disclose its limit.
    const value = maximumValue && requestedValue > maximumValue ? maximumValue : requestedValue;
    let filled = await fillScheduleInput(combined, value, rule.scheduleControl);
    if (filled && rule.scheduleControl.wechatVideoDateTimePicker) {
      // The controlled WeChat input can briefly echo a synthetic value before
      // Vue restores the platform state. Recheck once, then use only the
      // native calendar if that provisional write did not persist.
      await page.waitForTimeout(150);
      filled = await scheduleInputMatches(combined, value);
    }
    if (!filled && rule.scheduleControl.wechatVideoDateTimePicker) {
      filled = await chooseWechatVideoDateTimePicker(page, combined, parts);
    }
    if (!filled && rule.scheduleControl.antDateTimePicker) {
      filled = await chooseAntDateTimePicker(page, combined, parts);
    }
    if (filled) {
      if (rule.scheduleControl.requireCheckedChoice) {
        await page.waitForTimeout(100);
        if (!await scheduleInputMatches(combined, value) || !await exactScheduleChoiceChecked(page, rule.scheduleControl)) {
          return { applied: [], unverified: ['publish.time'], value: '', requestedValue, fallbackUsed: false, maximumValue };
        }
      }
      return {
        applied: ['publish.time'],
        unverified: [],
        value: scheduleDisplayValue(value, rule.scheduleControl),
        requestedValue,
        fallbackUsed: value !== requestedValue,
        maximumValue,
      };
    }
  }

  const [dateInput, timeInput] = await Promise.all([
    firstVisibleEventually(page, rule.scheduleControl.dateInputs || [], 3000),
    firstVisibleEventually(page, rule.scheduleControl.timeInputs || [], 3000),
  ]);
  if (dateInput && timeInput) {
    const maximumDate = scheduleDateMaximumValue(await scheduleMaximumAttribute(dateInput, rule.scheduleControl.maximumAttributes));
    const maximumTime = scheduleTimeMaximumValue(await scheduleMaximumAttribute(timeInput, rule.scheduleControl.maximumAttributes));
    const fallbackUsed = Boolean(maximumDate && parts.date > maximumDate);
    const dateValue = fallbackUsed ? maximumDate : parts.date;
    // A time max is a reviewed native constraint. It is used only when the
    // date had to fall back, so the effective date/time is the latest value
    // the form explicitly exposes rather than an invented end-of-day value.
    const timeValue = fallbackUsed && maximumTime ? maximumTime : parts.time;
    const maximumValue = maximumDate
      ? `${maximumDate}${maximumTime ? ` ${maximumTime}:00` : ''}`
      : '';
  if (await fillScheduleInput(dateInput, dateValue, rule.scheduleControl)
      && await fillScheduleInput(timeInput, timeValue, rule.scheduleControl)
      && await scheduleInputMatches(dateInput, dateValue)
      && await scheduleInputMatches(timeInput, timeValue)) {
      const value = `${dateValue} ${timeValue}:00`;
      return { applied: ['publish.time'], unverified: [], value, requestedValue: parts.dateTime, fallbackUsed, maximumValue };
    }
  }
  return { applied: [], unverified: ['publish.time'], value: '', requestedValue: parts.dateTime, fallbackUsed: false, maximumValue: '' };
}

function orderedMediaItems(items) {
  return [...items].sort((left, right) => left.position - right.position);
}

function frozenFileDescriptors(items) {
  try {
    return orderedMediaItems(items).map((item) => ({
      name: path.basename(item.path),
      size: fs.statSync(item.path).size,
    }));
  } catch (_) {
    return null;
  }
}

function mediaMIMEType(item) {
  const extension = path.extname(item.path).toLowerCase();
  const reviewed = {
    '.png': 'image/png',
    '.jpg': 'image/jpeg',
    '.jpeg': 'image/jpeg',
    '.webp': 'image/webp',
    '.bmp': 'image/bmp',
    '.tif': 'image/tiff',
    '.tiff': 'image/tiff',
  };
  return reviewed[extension] || (item.kind === 'video' ? 'video/mp4' : 'application/octet-stream');
}

function immutableFilePayloadBundle(items) {
  try {
    const payloads = [];
    const descriptors = [];
    const sha256s = [];
    for (const item of orderedMediaItems(items)) {
      const buffer = fs.readFileSync(item.path);
      const sha256 = crypto.createHash('sha256').update(buffer).digest('hex');
      if (sha256 !== item.sha256) return null;
      const name = path.basename(item.path);
      payloads.push({ name, mimeType: mediaMIMEType(item), buffer });
      descriptors.push({ name, size: buffer.length });
      sha256s.push(sha256);
    }
    return { payloads, descriptors, sha256s };
  } catch (_) {
    return null;
  }
}

async function selectedFileEvidence(control, items, expectedOverride = null) {
  const expectedFiles = expectedOverride || frozenFileDescriptors(items);
  if (!control || !expectedFiles) {
    return { verified: false, expectedFiles: expectedFiles || [], selectedFiles: [] };
  }
  const selectedFiles = await control.evaluate((node) => (
    [...(node.files || [])].map((file) => ({ name: file.name, size: file.size }))
  )).catch(() => []);
  const verified = selectedFiles.length === expectedFiles.length
    && selectedFiles.every((file, index) => (
      file.name === expectedFiles[index].name && file.size === expectedFiles[index].size
    ));
  return { verified, expectedFiles, selectedFiles };
}

async function observeFileSelectionBeforeNavigation(page, control) {
  if (!control) return async () => ({ files: null, capturedBeforeNavigation: false });
  const bindingName = `__alphaNexusFileSelection_${process.pid}_${++fileSelectionBindingSequence}`;
  let accepting = true;
  let initialFiles = null;
  let settledFiles = null;
  let navigated = false;
  let resolveSettled;
  const settledPromise = new Promise((resolve) => { resolveSettled = resolve; });
  const onFrameNavigated = (frame) => {
    if (frame === page.mainFrame()) navigated = true;
  };
  page.on('framenavigated', onFrameNavigated);
  try {
    await page.exposeBinding(bindingName, (_source, payload) => {
      if (!accepting) return;
      const files = Array.isArray(payload?.files) ? payload.files : null;
      if (payload?.phase === 'initial') {
        initialFiles = files;
        return;
      }
      if (payload?.phase === 'settled') {
        settledFiles = files;
        resolveSettled();
      }
    });
    await control.evaluate((node, name) => {
      node.addEventListener('change', (event) => {
        const notify = globalThis[name];
        if (typeof notify !== 'function') return;
        // Playwright's CDP file assignment emits an untrusted DOM change even
        // though the selected bytes are set by the browser protocol. Bind the
        // readback to this unique reviewed input and exact frozen descriptors.
        const target = event.currentTarget;
        const toFiles = () => [...(target.files || [])]
          .map((file) => ({ name: file.name, size: file.size }));
        Promise.resolve(notify({ phase: 'initial', files: toFiles() })).catch(() => {});
        // A same-document handler may replace input.files. Read it on the
        // next task, after every change handler has run. If the creator page
        // navigates before then, only the pre-navigation readback is retained;
        // upload completion still requires the platform's independent preview.
        setTimeout(() => {
          const files = [...(target.files || [])]
            .map((file) => ({ name: file.name, size: file.size }));
          Promise.resolve(notify({ phase: 'settled', files })).catch(() => {});
        }, 0);
      }, { capture: true, once: true });
    }, bindingName);
  } catch (_) {
    accepting = false;
    page.off('framenavigated', onFrameNavigated);
    return async () => ({ files: null, capturedBeforeNavigation: false });
  }
  return async () => {
    await Promise.race([
      settledPromise,
      new Promise((resolve) => setTimeout(resolve, 1000)),
    ]);
    accepting = false;
    page.off('framenavigated', onFrameNavigated);
    if (settledFiles) return {
      files: settledFiles, initialFiles, capturedBeforeNavigation: false,
    };
    return {
      files: navigated ? initialFiles : null, initialFiles,
      capturedBeforeNavigation: navigated && Array.isArray(initialFiles),
    };
  };
}

async function backgroundImageValue(control) {
  if (!control) return '';
  return control.evaluate((node) => (
    node.style.backgroundImage || getComputedStyle(node).backgroundImage || ''
  )).catch(() => '');
}

async function canvasSnapshotSignature(control) {
  if (!control) return '';
  return control.evaluate((canvas) => {
    if (!(canvas instanceof HTMLCanvasElement)) return '';
    try {
      const data = canvas.toDataURL('image/jpeg', 0.1);
      let hash = 0;
      for (let index = 0; index < data.length; index += 1) {
        hash = ((hash * 31) + data.charCodeAt(index)) | 0;
      }
      return `${canvas.width}x${canvas.height}:${data.length}:${hash}`;
    } catch (_) {
      return '';
    }
  }).catch(() => '');
}

async function applyNativeVideoCover(page, items, definition) {
  const nativeDefaultNames = definition.nativeDefaultNames || [];
  if ((!definition.requireExplicitUpload || definition.allowNativeDefaultWhenExplicitUnavailable)
    && nativeDefaultNames.length) {
    const deadline = Date.now() + 5000;
    do {
      for (const name of nativeDefaultNames) {
        const defaultState = page.getByText(name, { exact: true }).first();
        if (await defaultState.count() && await defaultState.isVisible().catch(() => false)) {
          return { applied: true, method: 'native_first_frame' };
        }
      }
      await page.waitForTimeout(250);
    } while (Date.now() < deadline);
  }
  if (definition.directFileChooser) {
    const previewSources = async () => {
      const sources = new Set();
      for (const selector of definition.committedPreviewSelectors || []) {
        const candidates = page.locator(selector);
        for (let index = 0; index < await candidates.count(); index += 1) {
          const candidate = candidates.nth(index);
          if (!await candidate.isVisible().catch(() => false)) continue;
          const source = await candidate.evaluate((node) => {
            if (node instanceof HTMLCanvasElement) {
              return node.width > 0 && node.height > 0 ? node.toDataURL('image/jpeg', 0.1) : '';
            }
            if (node instanceof HTMLImageElement) return node.currentSrc || node.getAttribute('src') || '';
            const background = node.style.backgroundImage || getComputedStyle(node).backgroundImage;
            return background && background !== 'none' ? background : '';
          }).catch(() => '');
          if (source) sources.add(source);
        }
      }
      return sources;
    };
    const before = await previewSources();
    let trigger = { control: null, ambiguous: false };
    const triggerDeadline = Date.now() + (definition.triggerTimeoutMs || 0);
    do {
      trigger = await uniqueVisibleNamedControl(page, definition.triggerNames || [], ['button']);
      if (!trigger.control && !trigger.ambiguous) {
        trigger = await uniqueVisibleExactText(page, definition.triggerNames || []);
      }
      if (trigger.control || trigger.ambiguous || Date.now() >= triggerDeadline) break;
      await page.waitForTimeout(250);
    } while (true);
    if (trigger.ambiguous || !trigger.control) return { applied: false, method: '' };
    const chooserPromise = page.waitForEvent('filechooser', { timeout: 2500 }).catch(() => null);
    if (!await trigger.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) {
      return { applied: false, method: '' };
    }
    const chooser = await chooserPromise;
    if (!chooser) return { applied: false, method: '' };
    try {
      await chooser.setFiles(orderedMediaItems(items).map((item) => item.path), { timeout: 3000 });
    } catch (_) {
      return { applied: false, method: '' };
    }
    const selection = await selectedFileEvidence(chooser.element(), items);
    if (!selection.verified) {
      return {
        applied: false,
        method: '',
        selectionVerified: false,
        expectedFiles: selection.expectedFiles,
        selectedFiles: selection.selectedFiles,
      };
    }
    const deadline = Date.now() + 10000;
    do {
      const after = await previewSources();
      if ([...after].some((source) => !before.has(source))) {
        return {
          applied: true,
          method: 'direct_file_chooser',
          selectionVerified: true,
          expectedFiles: selection.expectedFiles,
          selectedFiles: selection.selectedFiles,
          previewChanged: true,
          scopeVerified: true,
        };
      }
      if (Date.now() >= deadline) break;
      await page.waitForTimeout(100);
    } while (true);
    return {
      applied: false,
      method: '',
      selectionVerified: true,
      expectedFiles: selection.expectedFiles,
      selectedFiles: selection.selectedFiles,
    };
  }
  if (definition.requireExplicitUpload && definition.previewSelectors?.length) {
    const failed = (selection = {}) => ({
      applied: false,
      method: '',
      selectionVerified: selection.verified === true,
      expectedFiles: selection.expectedFiles || frozenFileDescriptors(items) || [],
      selectedFiles: selection.selectedFiles || [],
      previewChanged: false,
      scopeVerified: false,
      previewBackgroundBefore: '',
      previewBackgroundAfter: '',
    });
    const previewMatch = await uniqueVisible(page, definition.previewSelectors);
    if (!previewMatch.control || previewMatch.ambiguous) return failed();
    const beforeBackground = await backgroundImageValue(previewMatch.control);
    await previewMatch.control.hover({ timeout: 1500 }).catch(() => {});
    const triggerMatch = await uniqueVisible(page, definition.triggerSelectors || []);
    if (!triggerMatch.control || triggerMatch.ambiguous) return failed();
    await triggerMatch.control.click({ timeout: 1500 }).catch(() => {});

    const dialogDeadline = Date.now() + 3000;
    let dialogMatch = { control: null, ambiguous: false };
    do {
      dialogMatch = await uniqueVisible(page, definition.dialogSelectors || []);
      if (dialogMatch.control || dialogMatch.ambiguous || Date.now() >= dialogDeadline) break;
      await page.waitForTimeout(100);
    } while (true);
    if (!dialogMatch.control || dialogMatch.ambiguous) return failed();
    const uploads = dialogMatch.control.locator((definition.uploadSelectors || []).join(','));
    if (await uploads.count() !== 1) return failed();
    const upload = uploads.first();
    let selection;
    try {
      await upload.setInputFiles(orderedMediaItems(items).map((item) => item.path), { timeout: 3000 });
      selection = await selectedFileEvidence(upload, items);
    } catch (_) {
      return failed();
    }
    if (!selection.verified) return failed(selection);

    const confirms = dialogMatch.control.getByRole('button', {
      name: definition.confirmNames?.[0] || '',
      exact: true,
    });
    if (await confirms.count() !== 1 || !await confirms.first().isVisible().catch(() => false)) {
      return failed(selection);
    }
    await confirms.first().click({ timeout: 1500 }).catch(() => {});
    await dialogMatch.control.waitFor({ state: 'hidden', timeout: 5000 }).catch(() => {});
    if (await dialogMatch.control.isVisible().catch(() => false)) return failed(selection);

    const commitDeadline = Date.now() + 5000;
    let afterBackground = '';
    do {
      const currentPreview = await uniqueVisible(page, definition.previewSelectors);
      if (currentPreview.control && !currentPreview.ambiguous) {
        afterBackground = await backgroundImageValue(currentPreview.control);
        if (!definition.requirePreviewBackgroundChange
          || (afterBackground && afterBackground !== 'none' && afterBackground !== beforeBackground)) {
          return {
            applied: true,
            method: 'cover_upload_dialog',
            selectionVerified: true,
            expectedFiles: selection.expectedFiles,
            selectedFiles: selection.selectedFiles,
            previewChanged: true,
            scopeVerified: true,
            previewBackgroundBefore: beforeBackground,
            previewBackgroundAfter: afterBackground,
          };
        }
      }
      if (Date.now() >= commitDeadline) break;
      await page.waitForTimeout(100);
    } while (true);
    return failed(selection);
  }
  if (definition.requireExplicitUpload
    && !(
      definition.triggerSelectors?.length
      || definition.triggerNames?.length
      || definition.panelMarkerNames?.length
      || definition.confirmNames?.length
    )) {
    return {
      applied: false,
      method: '',
      selectionVerified: false,
      expectedFiles: frozenFileDescriptors(items) || [],
      selectedFiles: [],
    };
  }
  const visibleMarker = async () => {
    for (const name of definition.panelMarkerNames || []) {
      const marker = page.getByText(name, { exact: true }).first();
      if (await marker.count() && await marker.isVisible().catch(() => false)) return marker;
    }
    return null;
  };
  const visibleConfirm = async () => {
    for (const name of definition.confirmNames || []) {
      const confirm = page.getByRole('button', { name, exact: true }).first();
      if (await confirm.count() && await confirm.isVisible().catch(() => false)) return confirm;
      // Douyin's live cover editor renders “完成” as a clickable text node
      // without a button role. Scope this fallback to reviewed exact labels.
      const exactText = page.getByText(name, { exact: true }).first();
      if (await exactText.count() && await exactText.isVisible().catch(() => false)) return exactText;
    }
    return null;
  };
  let dialog = await firstVisible(page, ["[role='dialog']"]);
  let marker = await visibleMarker();
  let confirmMarker = await visibleConfirm();
  const deadline = Date.now() + 10000;
  if (!dialog && !confirmMarker) {
    let triggered = false;
    do {
      if (await clickExactTrigger(page, definition.triggerNames)) {
        triggered = true;
        break;
      }
      await page.waitForTimeout(500);
    } while (Date.now() < deadline);
    if (!triggered) return { applied: false, method: '' };
    dialog = await firstVisibleEventually(page, ["[role='dialog']"], 3000);
    marker = await visibleMarker();
    confirmMarker = await visibleConfirm();
  }
  if (!dialog && (!marker || !confirmMarker)) return { applied: false, method: '' };

  const scope = dialog || page;
  if (definition.uploadInputSelectors?.length) {
    let trigger = await uniqueVisibleNamedControl(scope, definition.uploadTriggerNames, ['button']);
    if (!trigger.control && !trigger.ambiguous) {
      trigger = await uniqueVisibleExactText(scope, definition.uploadTriggerNames);
    }
    if (trigger.ambiguous || !trigger.control) return { applied: false, method: '' };
    let uploadScope = trigger.control;
    for (let level = 0; level < (definition.uploadInputAncestorLevels || 0); level += 1) {
      uploadScope = uploadScope.locator('..');
    }
    const directUpload = uploadScope.locator(definition.uploadInputSelectors.join(','));
    if (await directUpload.count() !== 1) return { applied: false, method: '' };
    const payload = immutableFilePayloadBundle(items);
    if (!payload) return { applied: false, method: '' };
    const preview = await uniqueVisible(scope, definition.previewCanvasSelectors || []);
    if (!preview.control || preview.ambiguous) return { applied: false, method: '' };
    const beforePreview = await canvasSnapshotSignature(preview.control);
    if (definition.requirePreviewCanvasChange && !beforePreview) {
      return { applied: false, method: '' };
    }
    try {
      const files = definition.useVerifiedFilePaths
        ? orderedMediaItems(items).map((item) => item.path)
        : payload.payloads;
      await directUpload.first().setInputFiles(files, { timeout: 3000 });
    } catch (_) {
      return { applied: false, method: '' };
    }
    if (definition.useVerifiedFilePaths) {
      const afterUpload = immutableFilePayloadBundle(items);
      if (!afterUpload
        || afterUpload.sha256s.some((sha256, index) => sha256 !== payload.sha256s[index])) {
        return {
          applied: false,
          method: '',
          selectionVerified: false,
          expectedFiles: payload.descriptors,
          selectedFiles: [],
        };
      }
    }
    let afterPreview = '';
    let readyConfirm = null;
    const previewDeadline = Date.now() + (definition.uploadProcessingTimeoutMs || 10000);
    do {
      const currentPreview = await uniqueVisible(scope, definition.previewCanvasSelectors || []);
      if (currentPreview.control && !currentPreview.ambiguous) {
        afterPreview = await canvasSnapshotSignature(currentPreview.control);
        const previewReady = afterPreview
          && (!definition.requirePreviewCanvasChange || afterPreview !== beforePreview);
        if (previewReady) {
          const currentConfirm = await visibleConfirm();
          if (currentConfirm && await currentConfirm.isEnabled().catch(() => false)) {
            readyConfirm = currentConfirm;
            break;
          }
        }
      }
      if (Date.now() >= previewDeadline) break;
      await page.waitForTimeout(100);
    } while (true);
    if (!afterPreview || (definition.requirePreviewCanvasChange && afterPreview === beforePreview)
      || !readyConfirm) {
      return {
        applied: false,
        method: '',
        selectionVerified: true,
        expectedFiles: payload.descriptors,
        selectedFiles: payload.descriptors,
      };
    }
    if (!await readyConfirm.click({ timeout: 1500 }).then(() => true).catch(() => false)) {
      return { applied: false, method: '' };
    }
    await readyConfirm.waitFor({ state: 'hidden', timeout: 15000 }).catch(() => {});
    for (const postName of definition.postConfirmNames || []) {
      await clickNamed(page, [postName]);
    }
    if (await readyConfirm.isVisible().catch(() => false)) return { applied: false, method: '' };
    return {
      applied: true,
      method: 'cover_upload_dialog',
      selectionVerified: true,
      expectedFiles: payload.descriptors,
      selectedFiles: payload.descriptors,
      previewChanged: afterPreview !== beforePreview,
    };
  }
  const upload = scope.locator("input[type='file'][accept*='image' i]");
  const uploadCount = await upload.count();
  if (uploadCount === 1 && !definition.uploadTriggerNames?.length) {
    let selection;
    try {
      await upload.first().setInputFiles(orderedMediaItems(items).map((item) => item.path), { timeout: 3000 });
      selection = await selectedFileEvidence(upload.first(), items);
    } catch (_) {
      return { applied: false, method: '' };
    }
    if (definition.requireExplicitUpload && !selection.verified) {
      return {
        applied: false,
        method: '',
        selectionVerified: false,
        expectedFiles: selection.expectedFiles,
        selectedFiles: selection.selectedFiles,
      };
    }
    const confirm = await visibleConfirm();
    if (confirm) {
      await confirm.click({ timeout: 1500 }).catch(() => {});
      await confirm.waitFor({ state: 'hidden', timeout: 5000 }).catch(() => {});
      for (const postName of definition.postConfirmNames || []) {
        await clickNamed(page, [postName]);
      }
      if (!await confirm.isVisible().catch(() => false)) {
        return {
          applied: true,
          method: 'cover_upload_dialog',
          selectionVerified: selection.verified,
          expectedFiles: selection.expectedFiles,
          selectedFiles: selection.selectedFiles,
        };
      }
      return { applied: false, method: '' };
    }
    // Some reviewed upload panels apply the image and close immediately.
    // A still-visible modal means confirmation is unresolved and must stop.
    if (dialog && await dialog.isVisible().catch(() => false)) {
      return { applied: false, method: '' };
    }
    return {
      applied: true,
      method: 'cover_upload_dialog',
      selectionVerified: selection.verified,
      expectedFiles: selection.expectedFiles,
      selectedFiles: selection.selectedFiles,
    };
  }
  if (definition.uploadTriggerNames?.length) {
    const imageSourcesBefore = new Set();
    const imagesBefore = scope.locator('img[src]');
    for (let index = 0; index < await imagesBefore.count(); index += 1) {
      const image = imagesBefore.nth(index);
      if (!await image.isVisible().catch(() => false)) continue;
      const source = await image.getAttribute('src').catch(() => '');
      if (source) imageSourcesBefore.add(source);
    }
    let trigger = await uniqueVisibleNamedControl(scope, definition.uploadTriggerNames, ['button']);
    if (!trigger.control && !trigger.ambiguous) {
      trigger = await uniqueVisibleExactText(scope, definition.uploadTriggerNames);
    }
    if (trigger.ambiguous || !trigger.control) return { applied: false, method: '' };
    const chooserPromise = page.waitForEvent('filechooser', { timeout: 2500 }).catch(() => null);
    if (!await trigger.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) {
      return { applied: false, method: '' };
    }
    const chooser = await chooserPromise;
    if (!chooser) return { applied: false, method: '' };
    try {
      await chooser.setFiles(orderedMediaItems(items).map((item) => item.path), { timeout: 3000 });
    } catch (_) {
      return { applied: false, method: '' };
    }
    const selection = await selectedFileEvidence(chooser.element(), items);
    if (!selection.verified) {
      return {
        applied: false,
        method: '',
        selectionVerified: false,
        expectedFiles: selection.expectedFiles,
        selectedFiles: selection.selectedFiles,
      };
    }
    let confirm = null;
    let uploadedCandidateClicked = false;
    const confirmDeadline = Date.now() + 10000;
    do {
      const candidate = await visibleConfirm();
      if (candidate
        && (!definition.selectUploadedCandidate || uploadedCandidateClicked)
        && await candidate.isEnabled().catch(() => false)) {
        confirm = candidate;
        break;
      }
      if (!uploadedCandidateClicked) {
        if (definition.uploadedCandidateSelectors?.length) {
          const uploadedCandidate = await uniqueVisible(scope, definition.uploadedCandidateSelectors);
          if (uploadedCandidate.ambiguous) {
            return {
              applied: false,
              method: '',
              selectionVerified: true,
              expectedFiles: selection.expectedFiles,
              selectedFiles: selection.selectedFiles,
            };
          }
          if (uploadedCandidate.control) {
            const clickTarget = definition.uploadedCandidateClickParent
              ? uploadedCandidate.control.locator('..')
              : uploadedCandidate.control;
            uploadedCandidateClicked = await clickTarget
              .click({ timeout: 1500 })
              .then(() => true)
              .catch(() => false);
            if (uploadedCandidateClicked && definition.uploadedCandidateSelectedClassPrefix) {
              const selectedClass = await clickTarget.getAttribute('class').catch(() => '');
              uploadedCandidateClicked = String(selectedClass || '')
                .split(/\s+/)
                .some((name) => name.startsWith(definition.uploadedCandidateSelectedClassPrefix));
            }
          }
        }
        if (uploadedCandidateClicked) continue;
        const images = scope.locator('img[src]');
        const newImagesBySource = new Map();
        for (let index = 0; index < await images.count(); index += 1) {
          const image = images.nth(index);
          if (!await image.isVisible().catch(() => false)) continue;
          const source = await image.getAttribute('src').catch(() => '');
          const box = await image.boundingBox().catch(() => null);
          if (!source || imageSourcesBefore.has(source) || !box || box.width < 40 || box.height < 40) continue;
          if (!newImagesBySource.has(source)) newImagesBySource.set(source, image);
        }
        if (newImagesBySource.size === 1) {
          uploadedCandidateClicked = await [...newImagesBySource.values()][0]
            .click({ timeout: 1500 })
            .then(() => true)
            .catch(() => false);
        } else if (newImagesBySource.size === 0 && definition.selectUploadedCandidate) {
          const scopeBox = await scope.boundingBox().catch(() => null);
          const leftPanelImagesBySource = new Map();
          if (scopeBox) {
            for (let index = 0; index < await images.count(); index += 1) {
              const image = images.nth(index);
              if (!await image.isVisible().catch(() => false)) continue;
              const source = await image.getAttribute('src').catch(() => '');
              const box = await image.boundingBox().catch(() => null);
              if (!source || !box || box.width < 120 || box.height < 120) continue;
              const centerX = box.x + (box.width / 2);
              if (centerX > scopeBox.x + (scopeBox.width * 0.35)) continue;
              if (!leftPanelImagesBySource.has(source)) leftPanelImagesBySource.set(source, image);
            }
          }
          if (leftPanelImagesBySource.size === 1) {
            uploadedCandidateClicked = await [...leftPanelImagesBySource.values()][0]
              .click({ timeout: 1500 })
              .then(() => true)
              .catch(() => false);
          }
        }
      }
      if (Date.now() >= confirmDeadline) break;
      await page.waitForTimeout(100);
    } while (true);
    if (!confirm) {
      return {
        applied: false,
        method: '',
        selectionVerified: true,
        expectedFiles: selection.expectedFiles,
        selectedFiles: selection.selectedFiles,
      };
    }
    if (!await confirm.click({ timeout: 1500 }).then(() => true).catch(() => false)) {
      return { applied: false, method: '' };
    }
    await confirm.waitFor({ state: 'hidden', timeout: 5000 }).catch(() => {});
    for (const postName of definition.postConfirmNames || []) {
      await clickNamed(page, [postName]);
    }
    if (!await confirm.isVisible().catch(() => false)) {
      return {
        applied: true,
        method: 'cover_upload_dialog',
        selectionVerified: true,
        expectedFiles: selection.expectedFiles,
        selectedFiles: selection.selectedFiles,
      };
    }
    return { applied: false, method: '' };
  }
  const confirm = await visibleConfirm();
  if (confirm) {
    await confirm.click({ timeout: 1500 }).catch(() => {});
    await confirm.waitFor({ state: 'hidden', timeout: 5000 }).catch(() => {});
    for (const postName of definition.postConfirmNames || []) {
      await clickNamed(page, [postName]);
    }
    if (!await confirm.isVisible().catch(() => false)) {
      return { applied: true, method: 'native_first_frame' };
    }
  }
  return { applied: false, method: '' };
}

async function nativeArticleCoverPreviewCommitted(page, definition, timeoutMs = 5000) {
  const deadline = Date.now() + timeoutMs;
  do {
    for (const selector of definition.committedSelectors || []) {
      const committed = page.locator(selector).first();
      if (await committed.count() && await committed.isVisible().catch(() => false)) return true;
    }
    await page.waitForTimeout(100);
  } while (Date.now() < deadline);
  return false;
}

async function exactArticleCoverChoice(page, definition) {
  for (const selector of definition.choiceOpenRootSelectors || []) {
    const roots = page.locator(selector);
    const count = await roots.count();
    if (count > 1) return { control: null, ambiguous: true };
    if (count === 1) {
      const matches = roots.first().locator((definition.choiceSelectors || []).join(','));
      const choiceCount = await matches.count();
      if (choiceCount === 1) return { control: matches.first(), ambiguous: false };
      if (choiceCount > 1) return { control: null, ambiguous: true };
    }
  }
  if (definition.choiceRootSelectors?.length) {
    const root = await uniqueVisible(page, definition.choiceRootSelectors);
    if (root.ambiguous) return { control: null, ambiguous: true };
    if (root.control) {
      const matches = root.control.locator((definition.choiceSelectors || []).join(','));
      const count = await matches.count();
      if (count === 1) return { control: matches.first(), ambiguous: false };
      if (count > 1) return { control: null, ambiguous: true };
    }
  }
  let choice = await uniqueVisible(page, definition.choiceSelectors || []);
  if (!choice.control && !choice.ambiguous) {
    choice = await uniqueVisibleExactText(page, definition.choiceNames || []);
  }
  return choice;
}

async function applyArticleCover(
  page,
  definition,
  items = [],
  attempt = 0,
  emptyPickerAttempt = 0,
) {
  if (definition.source === 'native_upload') {
    const trigger = await firstVisibleEventually(page, definition.triggerSelectors || [], 3000);
    if (definition.triggerSelectors?.length && !trigger) {
      return { applied: false, selectionVerified: false, previewCommitted: false };
    }
    const selectFrozenFiles = async (control, setFiles) => {
      const readObservedFiles = await observeFileSelectionBeforeNavigation(page, control);
      await setFiles();
      const observed = await readObservedFiles();
      const direct = await selectedFileEvidence(control, items);
      const observedFiles = exactFileDescriptorSet(observed.files, direct.expectedFiles)
        ? observed.files
        : exactFileDescriptorSet(observed.initialFiles, direct.expectedFiles)
          ? observed.initialFiles
          : direct.selectedFiles;
      return {
        ...direct,
        selectedFiles: observedFiles,
        verified: exactFileDescriptorSet(observedFiles, direct.expectedFiles),
      };
    };
    const commitNativeUpload = async () => {
      // CSDN's crop component delegates the actual cover write to its native
      // uploader. Keep only a bounded, non-sensitive transport outcome so a
      // missing preview can be distinguished from an upload the platform
      // rejected; never retain request URLs, bodies, headers, or responses.
      const uploadStatuses = [];
      const observeUpload = (response) => {
        const request = response.request();
        const url = response.url();
        if (!['POST', 'PUT'].includes(request.method()) || !/upload|image/i.test(url)) return;
        uploadStatuses.push(response.status());
      };
      page.on('response', observeUpload);
      const finish = (result) => {
        page.off('response', observeUpload);
        return {
          ...result,
          nativeUploadObserved: uploadStatuses.length > 0,
          nativeUploadStatuses: [...new Set(uploadStatuses)].slice(0, 3),
        };
      };
      let confirm = await uniqueVisibleExactText(
        page,
        definition.nativeUploadConfirmNames || [],
        definition.nativeUploadConfirmSelectors || [],
      );
      if (!confirm.control && !confirm.ambiguous) {
        confirm = await uniqueVisibleNamedControl(
          page,
          definition.nativeUploadConfirmNames || [],
          ['button'],
        );
      }
      if (confirm.ambiguous || !confirm.control) return finish({ committed: false, needsReopenVerification: false });
      if (!await confirm.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) {
        return finish({ committed: false, needsReopenVerification: false });
      }
      const deadline = Date.now() + (definition.commitTimeoutMs || 5000);
      do {
        if (!await confirm.control.isVisible().catch(() => false)) {
          // CSDN closes the options dialog before its native direct upload
          // updates `preview-box`. Reopen only the non-terminal options
          // surface, then wait there for the real saved-cover state.
          if (definition.reopenOptionsAfterCommit === true) {
            await page.waitForTimeout(1000);
            return finish({ committed: false, needsReopenVerification: true });
          }
          if (await nativeArticleCoverPreviewCommitted(page, definition, definition.commitTimeoutMs || 5000)) {
            return finish({ committed: true, needsReopenVerification: false });
          }
          return finish({ committed: false, needsReopenVerification: definition.reopenOptionsAfterCommit === true });
        }
        await page.waitForTimeout(100);
      } while (Date.now() < deadline);
      return finish({ committed: false, needsReopenVerification: false });
    };
    if (trigger) {
      // The visible CSDN action owns the currently active input. Prefer its
      // native file chooser so the page receives the same event sequence as
      // an operator selecting “从本地上传”; do not assume a hidden input is
      // still mounted after that click.
      const chooserPromise = page.waitForEvent('filechooser', { timeout: 1500 }).catch(() => null);
      if (!await trigger.click({ timeout: 1500 }).then(() => true).catch(() => false)) {
        return { applied: false, selectionVerified: false, previewCommitted: false };
      }
      const chooser = await chooserPromise;
      if (chooser) {
        try {
          const selection = await selectFrozenFiles(
            chooser.element(),
            () => chooser.setFiles(orderedMediaItems(items).map((item) => item.path)),
          );
          if (selection.verified) {
            const committed = await commitNativeUpload();
            return {
              applied: committed.committed, selectionVerified: true,
              previewCommitted: committed.committed,
              needsReopenVerification: committed.needsReopenVerification,
              nativeUploadObserved: committed.nativeUploadObserved,
              nativeUploadStatuses: committed.nativeUploadStatuses,
            };
          }
        } catch (_) {
          // Fall through to an explicitly reviewed input only when the native
          // chooser itself did not retain the immutable file selection.
        }
      }
      await page.waitForTimeout(200);
    }
    const selectorGroups = definition.uploadSelectorGroups || [definition.uploadSelectors || []];
    let retainedSelection = false;
    for (const selectors of selectorGroups) {
      const upload = page.locator(selectors.join(','));
      if (await upload.count() !== 1) continue;
      let selection;
      try {
        selection = await selectFrozenFiles(
          upload,
          () => upload.setInputFiles(orderedMediaItems(items).map((item) => item.path), { timeout: 3000 }),
        );
      } catch (_) {
        continue;
      }
      if (!selection.verified) continue;
      retainedSelection = true;
      const committed = await commitNativeUpload();
      if (committed.committed || committed.needsReopenVerification) {
        return {
          applied: committed.committed, selectionVerified: true,
          previewCommitted: committed.committed,
          needsReopenVerification: committed.needsReopenVerification,
          nativeUploadObserved: committed.nativeUploadObserved,
          nativeUploadStatuses: committed.nativeUploadStatuses,
        };
      }
    }
    return { applied: false, selectionVerified: retainedSelection, previewCommitted: false };
  }
  const trigger = await firstVisibleEventually(page, definition.triggerSelectors || [], 3000);
  if (!trigger) return false;
  let existingChoice = { control: null, ambiguous: false };
  if (definition.choiceNames?.length) {
    existingChoice = await exactArticleCoverChoice(page, definition);
    if (existingChoice.ambiguous) return false;
  }
  const coverCommitted = async () => {
    for (const selector of definition.committedSelectors || []) {
      const committed = page.locator(selector).first();
      if (await committed.count() && await committed.isVisible().catch(() => false)) return true;
    }
    const media = trigger.locator('img, canvas, video').first();
    if (await media.count() && await media.isVisible().catch(() => false)) return true;
    const inlineStyle = await trigger.getAttribute('style').catch(() => '');
    return /background(?:-image)?\\s*:/i.test(inlineStyle || '');
  };
  if (!existingChoice.control) {
    const triggerClicked = await trigger.click({ timeout: 1500 })
      .then(() => true)
      .catch(() => false);
    if (!triggerClicked) {
      // WeChat's sticky bottom toolbar can overlap this already-reviewed,
      // non-terminal cover control after the first cropper closes. Dispatching
      // the element's native click keeps the retry scoped to that exact control
      // without moving the user's pointer or broad coordinate guessing.
      await trigger.evaluate((node) => node.click()).catch(() => {});
    }
  }
  if (definition.choiceNames?.length) {
    // The official editor retains hidden duplicate menu labels. Selecting the
    // first text match can therefore miss the visible cover-source action.
    // Its current button also includes a nested “可选视频封面” hint, so exact
    // text is only a fallback after the reviewed native selector.
    const choice = existingChoice.control
      ? existingChoice
      : await exactArticleCoverChoice(page, definition);
    if (choice.ambiguous || !choice.control) {
      return false;
    }
    let choiceClicked = await choice.control.click({ timeout: 1500 })
      .then(() => true)
      .catch(() => false);
    if (!choiceClicked) {
      choiceClicked = await choice.control.click({ force: true, timeout: 1500 })
        .then(() => true)
        .catch(() => false);
    }
    if (!choiceClicked) {
      // The fixed editor footer can intercept this exact, already-reviewed
      // non-terminal menu item. DOM click is a bounded fallback that neither
      // guesses coordinates nor moves the user's system pointer.
      choiceClicked = await choice.control.evaluate((node) => {
        node.click();
        return true;
      }).catch(() => false);
    }
    if (!choiceClicked) return false;
  }
  let panel = await firstVisibleEventually(page, definition.panelSelectors || [], 3000);
  if (!panel && definition.choiceNames?.length) {
    // A force-dispatched click can be acknowledged by CDP while the covered
    // menu item itself receives no event. Verify the expected picker, then
    // dispatch the exact element's DOM click once before the bounded retry.
    const reviewedChoice = await exactArticleCoverChoice(page, definition);
    if (!reviewedChoice.ambiguous && reviewedChoice.control) {
      await reviewedChoice.control.evaluate((node) => node.click()).catch(() => {});
      panel = await firstVisibleEventually(page, definition.panelSelectors || [], 1000);
    }
  }
  const choiceRetryAttempts = Number.isInteger(definition.choiceRetryAttempts)
    ? definition.choiceRetryAttempts
    : definition.retryChoiceOnce ? 1 : 0;
  for (let retry = 0; !panel && retry < choiceRetryAttempts
    && definition.choiceNames?.length; retry += 1) {
    // The source menu can become visible before the editor attaches its
    // picker action after a fresh body-image upload. Keep the menu open, wait
    // for hydration, and re-dispatch that same exact non-terminal item within
    // a small fixed budget.
    await page.waitForTimeout(definition.choiceRetryDelayMs || 1000);
    const reviewedChoice = await exactArticleCoverChoice(page, definition);
    if (!reviewedChoice.ambiguous && reviewedChoice.control) {
      await reviewedChoice.control.evaluate((node) => node.click()).catch(() => {});
      panel = await firstVisibleEventually(page, definition.panelSelectors || [], 3000);
    }
  }
  if (!panel) return false;
  let candidates = null;
  for (const selector of definition.candidateSelectors || []) {
    const reviewed = panel.locator(selector);
    if (await reviewed.count()) {
      candidates = reviewed;
      break;
    }
  }
  if (!candidates) candidates = panel.locator('img');
  let selected = false;
  for (let index = 0; index < await candidates.count(); index += 1) {
    const candidate = candidates.nth(index);
    if (!await candidate.isVisible().catch(() => false)) continue;
    await candidate.click({ timeout: 1500 }).catch(() => {});
    selected = true;
    break;
  }
  if (!selected) {
    if (definition.retryEmptyPickerOnce && emptyPickerAttempt === 0) {
      // The official picker can snapshot the article body before its uploaded
      // image finishes hydrating into the cover-source list. Closing and
      // reopening this non-terminal picker refreshes that list without
      // changing the article or consuming the one allowed crop-commit retry.
      const cancel = panel.getByRole('button', { name: '取消', exact: true }).first();
      if (await cancel.count() && await cancel.isVisible().catch(() => false)) {
        await cancel.click({ timeout: 1500 }).catch(() => {});
        await cancel.waitFor({ state: 'hidden', timeout: 3000 }).catch(() => {});
        await page.waitForTimeout(300);
        return applyArticleCover(page, definition, items, attempt, 1);
      }
    }
    return false;
  }
  for (const name of definition.nextNames || []) {
    const next = panel.getByRole('button', { name, exact: true }).first();
    if (!await next.count() || !await next.isVisible().catch(() => false)) return false;
    await next.click({ timeout: 1500 }).catch(() => {});
    await page.waitForTimeout(250);
    if (await next.isVisible().catch(() => false)) return false;
  }
  // The native cropper can remain on an “编辑封面” loading surface for
  // several seconds after the reviewed body image is selected. Wait for one
  // exact terminal control instead of turning that platform wait into a
  // false missing-cover result.
  const confirmationDeadline = Date.now() + (definition.commitTimeoutMs || 3000);
  let confirmation = { control: null, ambiguous: false };
  do {
    confirmation = await uniqueVisibleExactText(page, definition.confirmNames || []);
    if (confirmation.ambiguous) return false;
    if (confirmation.control || Date.now() >= confirmationDeadline) break;
    await page.waitForTimeout(100);
  } while (true);
  const confirm = confirmation.control;
  if (!confirm) return false;
  await confirm.click({ timeout: 1500 }).catch(() => {});
  const deadline = Date.now() + (definition.commitTimeoutMs || 3000);
  do {
    if (!await confirm.isVisible().catch(() => false) && await coverCommitted()) return true;
    await page.waitForTimeout(100);
  } while (Date.now() < deadline);
  if (definition.retryUncommittedOnce && attempt === 0) {
    return applyArticleCover(page, definition, items, 1, emptyPickerAttempt);
  }
  return false;
}

function recordArticleCoverEvidence(evidence, result) {
  const applied = typeof result === 'boolean' ? result : Boolean(result?.applied);
  evidence.articleCoverApplied = applied;
  if (typeof result !== 'boolean') {
    evidence.articleCoverSelectionVerified = Boolean(result.selectionVerified);
    evidence.articleCoverPreviewCommitted = Boolean(result.previewCommitted);
    evidence.articleCoverNativeUploadObserved = Boolean(result.nativeUploadObserved);
    evidence.articleCoverNativeUploadStatuses = Array.isArray(result.nativeUploadStatuses)
      ? result.nativeUploadStatuses
      : [];
  }
  return applied;
}

async function uniqueVisibleScope(page, selector) {
  if (!selector) return { scope: page, verified: true, ambiguous: false };
  const matches = page.locator(selector);
  const count = await matches.count();
  if (count !== 1) return { scope: null, verified: false, ambiguous: count > 1 };
  const scope = matches.first();
  if (!await scope.isVisible().catch(() => false)) return { scope: null, verified: false, ambiguous: false };
  return { scope, verified: true, ambiguous: false };
}

async function reviewedUploadSnapshot(page, rule) {
  const resolved = await uniqueVisibleScope(page, rule.uploadRootSelector);
  if (!resolved.verified) {
    return {
      scopeVerified: false,
      fingerprints: [],
      progressCleared: false,
      errorFree: false,
    };
  }
  const scope = resolved.scope;
  const fingerprints = await scope.locator('img, video, canvas').evaluateAll((nodes, sourcePattern) => {
    const seenSources = new Set();
    return nodes.map((node, index) => {
      const rect = node.getBoundingClientRect();
      const style = getComputedStyle(node);
      if (style.display === 'none' || style.visibility === 'hidden' || rect.width < 96 || rect.height < 72) return '';
      const source = node.currentSrc || node.src || node.poster || '';
      if (sourcePattern && (!source || !new RegExp(sourcePattern).test(source))) return '';
      if (sourcePattern && seenSources.has(source)) return '';
      if (node.tagName === 'IMG' && (!node.complete || node.naturalWidth <= 0 || node.naturalHeight <= 0)) return '';
      if (node.tagName === 'VIDEO' && node.readyState < 2) return '';
      if (sourcePattern) seenSources.add(source);
      return `${node.tagName}:${index}:${Math.round(rect.width)}x${Math.round(rect.height)}:${source}`;
    })
      .filter(Boolean);
  }, rule.uploadPreviewSourcePattern || '').catch(() => []);
  // Native upload-ready markers are independent evidence, not merely a
  // fallback for pages without media nodes. Composer artwork can exist before
  // selection; adding READY after selection must still count as a new proof.
  for (const name of rule.uploadReadyNames || []) {
    const readyControl = scope.getByText(name, { exact: true }).first();
    if (await readyControl.count() && await readyControl.isVisible().catch(() => false)) {
      fingerprints.push(`READY:${name}`);
      break;
    }
  }
  const statusScope = rule.uploadStatusPageWide ? page : scope;
  const progress = statusScope.locator("[role='progressbar'], [aria-busy='true']");
  let progressVisible = false;
  for (let index = 0; index < await progress.count(); index += 1) {
    if (await progress.nth(index).isVisible().catch(() => false)) {
      progressVisible = true;
      break;
    }
  }
  const processingText = statusScope.getByText(
    /^(uploading|processing|transcoding|正在上传|上传中|处理中|转码中)(?:\s+\d{1,3}%?)?[.….\s]*$/i,
  );
  for (let index = 0; index < await processingText.count(); index += 1) {
    if (await processingText.nth(index).isVisible().catch(() => false)) {
      progressVisible = true;
      break;
    }
  }
  const uploadError = statusScope.getByText(/(upload failed|processing failed|file is invalid|上传失败|处理失败|文件损坏)/i);
  let errorVisible = false;
  for (let index = 0; index < await uploadError.count(); index += 1) {
    if (await uploadError.nth(index).isVisible().catch(() => false)) {
      errorVisible = true;
      break;
    }
  }
  return {
    scopeVerified: true,
    fingerprints,
    progressCleared: !progressVisible,
    errorFree: !errorVisible,
  };
}

async function reviewedUploadSnapshotEventually(page, rule, timeoutMs) {
  const deadline = Date.now() + Math.max(0, timeoutMs);
  let snapshot;
  do {
    snapshot = await reviewedUploadSnapshot(page, rule);
    if (snapshot.scopeVerified || Date.now() >= deadline) return snapshot;
    await page.waitForTimeout(100);
  } while (true);
}

async function stableReviewedUploadSnapshot(page, rule, timeoutMs) {
  const stableMs = Math.max(0, rule.uploadCompletionStableMs || 0);
  const deadline = Date.now() + Math.max(stableMs, timeoutMs);
  let snapshot = await reviewedUploadSnapshot(page, rule);
  let stableSince = 0;
  let fingerprintKey = '';
  do {
    const currentKey = snapshot.fingerprints.join('\n');
    const ready = snapshot.scopeVerified
      && snapshot.fingerprints.length > 0
      && snapshot.progressCleared
      && snapshot.errorFree;
    if (ready) {
      if (currentKey !== fingerprintKey) {
        fingerprintKey = currentKey;
        stableSince = Date.now();
      }
      if (Date.now() - stableSince >= stableMs) {
        return { snapshot, stable: true };
      }
    } else {
      fingerprintKey = '';
      stableSince = 0;
    }
    if (Date.now() >= deadline) break;
    await page.waitForTimeout(100);
    snapshot = await reviewedUploadSnapshot(page, rule);
  } while (true);
  return { snapshot, stable: false };
}

async function uploadRequiredMedia(page, input, rule) {
  const evidence = {};
  const missing = [];
  // This destructive reset is platform-scoped and only enabled after the
  // caller explicitly authorizes replacing an existing Kuaishou draft.
  if (rule.clearExistingMediaNames?.length && await clickExactTrigger(page, rule.clearExistingMediaNames)) {
    evidence.existingMediaCleared = true;
    await page.waitForTimeout(300);
  }
  const uploadState = async (before, timeoutMs, requiredPreviewIncrement) => {
    if (!before.scopeVerified && !rule.uploadRootMayMountAfterSelection) {
      return {
        previewObserved: false,
        progressCleared: false,
        errorFree: false,
        scopeVerified: false,
        baselineMediaCount: before.fingerprints.length,
        previewMediaCount: before.fingerprints.length,
        previewIncrement: 0,
        previewFingerprints: [],
        completionStable: false,
      };
    }
    const deadline = Date.now() + timeoutMs;
    const stableMs = Math.max(0, rule.uploadCompletionStableMs || 0);
    let previewObserved = false;
    let previewIncrement = 0;
    let progressCleared = false;
    let errorFree = false;
    let scopeVerified = false;
    let previewMediaCount = 0;
    let previewFingerprints = [];
    let previewReplacementCount = 0;
    let completionStable = false;
    let stableSince = 0;
    let stableFingerprintKey = '';
    do {
      const after = await reviewedUploadSnapshot(page, rule);
      scopeVerified = after.scopeVerified
        && (before.scopeVerified || rule.uploadRootMayMountAfterSelection === true);
      previewMediaCount = after.fingerprints.length;
      previewFingerprints = after.fingerprints;
      previewIncrement = Math.max(
        0,
        previewMediaCount - (before.scopeVerified ? before.fingerprints.length : 0),
      );
      const beforeFingerprints = new Set(before.fingerprints);
      previewReplacementCount = after.fingerprints.filter((fingerprint) => !beforeFingerprints.has(fingerprint)).length;
      previewObserved = scopeVerified
        && Math.max(previewIncrement, previewReplacementCount) >= requiredPreviewIncrement;
      progressCleared = scopeVerified && after.progressCleared;
      errorFree = scopeVerified && after.errorFree;
      if (previewObserved && progressCleared && errorFree) {
        const currentKey = after.fingerprints.join('\n');
        if (currentKey !== stableFingerprintKey) {
          stableFingerprintKey = currentKey;
          stableSince = Date.now();
        }
        completionStable = Date.now() - stableSince >= stableMs;
      } else {
        stableFingerprintKey = '';
        stableSince = 0;
        completionStable = false;
      }
      if (completionStable) break;
      if (Date.now() >= deadline) break;
      await page.waitForTimeout(100);
    } while (true);
    return {
      previewObserved,
      progressCleared,
      errorFree,
      scopeVerified,
      baselineMediaCount: before.fingerprints.length,
      previewMediaCount,
      previewIncrement,
      previewFingerprints,
      previewReplacementCount,
      completionStable,
    };
  };
  const recordUploadState = async (evidenceKey, before, items) => {
    const requiredPreviewIncrement = rule.requirePreviewIncrementPerItem ? items.length : 1;
    const fallbackTimeoutMs = input.action === 'submit' ? 5000 : 400;
    const timeoutMs = rule.uploadCompletionTimeoutMs
      ? Math.min(input.timeoutMs, rule.uploadCompletionTimeoutMs)
      : fallbackTimeoutMs;
    const state = await uploadState(
      before,
      timeoutMs,
      requiredPreviewIncrement,
    );
    evidence[`${evidenceKey}PreviewObserved`] = state.previewObserved;
    evidence[`${evidenceKey}ProgressCleared`] = state.progressCleared;
    evidence[`${evidenceKey}ErrorFree`] = state.errorFree;
    evidence[`${evidenceKey}ScopeVerified`] = state.scopeVerified;
    evidence[`${evidenceKey}BaselineMediaCount`] = state.baselineMediaCount;
    evidence[`${evidenceKey}PreviewMediaCount`] = state.previewMediaCount;
    evidence[`${evidenceKey}PreviewIncrement`] = state.previewIncrement;
    evidence[`${evidenceKey}PreviewFingerprints`] = state.previewFingerprints;
    evidence[`${evidenceKey}PreviewReplacementCount`] = state.previewReplacementCount;
    evidence[`${evidenceKey}CompletionStable`] = state.completionStable;
    evidence[`${evidenceKey}ExpectedItemCount`] = items.length;
  };
  const recordFileSelection = async (
    evidenceKey,
    control,
    items,
    observedFiles = null,
    expectedOverride = null,
  ) => {
    const expectedFiles = expectedOverride || frozenFileDescriptors(items) || [];
    const selection = Array.isArray(observedFiles)
      ? {
        verified: exactFileDescriptorSet(observedFiles, expectedFiles),
        expectedFiles,
        selectedFiles: observedFiles,
      }
      : await selectedFileEvidence(control, items, expectedFiles);
    evidence[`${evidenceKey}SelectionVerified`] = selection.verified;
    evidence[`${evidenceKey}ExpectedFiles`] = selection.expectedFiles;
    evidence[`${evidenceKey}SelectedFiles`] = selection.selectedFiles;
    return !rule.requireFileSelectionReadback || selection.verified;
  };
  const upload = async (kind, items, name, evidenceKey) => {
    if (!items.length) return;
    if (name === 'media.cover' && rule.coverAfterPostUploadDialogs) return;
    const immutableBundle = rule.immutableMediaPayload
      ? immutableFilePayloadBundle(items)
      : null;
    if (rule.immutableMediaPayload && !immutableBundle) {
      evidence[`${evidenceKey}ImmutablePayloadRequired`] = true;
      evidence[`${evidenceKey}SHA256Verified`] = false;
      evidence[evidenceKey] = false;
      missing.push(name);
      return;
    }
    const filesToUpload = immutableBundle
      ? immutableBundle.payloads
      : orderedMediaItems(items).map((item) => item.path);
    const expectedFiles = immutableBundle?.descriptors || frozenFileDescriptors(items) || [];
    if (immutableBundle) {
      evidence[`${evidenceKey}ImmutablePayloadRequired`] = true;
      evidence[`${evidenceKey}SHA256Verified`] = true;
      evidence[`${evidenceKey}SHA256s`] = immutableBundle.sha256s;
    }
    const previewBefore = rule.uploadRootMayMountAfterSelection
      ? await reviewedUploadSnapshot(page, rule)
      : await reviewedUploadSnapshotEventually(page, rule, Math.min(input.timeoutMs, 5000));
    if (name === 'media.cover' && rule.coverControl) {
      const nativeCover = await applyNativeVideoCover(page, items, rule.coverControl);
      evidence[`${evidenceKey}SelectionVerified`] = nativeCover.selectionVerified === true;
      evidence[`${evidenceKey}ExpectedFiles`] = nativeCover.expectedFiles || frozenFileDescriptors(items) || [];
      evidence[`${evidenceKey}SelectedFiles`] = nativeCover.selectedFiles || [];
      evidence[`${evidenceKey}PreviewBackgroundBefore`] = nativeCover.previewBackgroundBefore || '';
      evidence[`${evidenceKey}PreviewBackgroundAfter`] = nativeCover.previewBackgroundAfter || '';
      evidence[evidenceKey] = nativeCover.applied
        && (
          nativeCover.method === 'native_first_frame'
          || !rule.requireFileSelectionReadback
          || nativeCover.selectionVerified === true
        );
      evidence.coverAppliedAs = nativeCover.method;
      if (nativeCover.applied) {
        if (nativeCover.method !== 'native_first_frame') {
          if (rule.coverControl.requireExplicitUpload && nativeCover.previewChanged) {
            const after = await reviewedUploadSnapshot(page, rule);
            evidence[`${evidenceKey}PreviewObserved`] = true;
            evidence[`${evidenceKey}ProgressCleared`] = after.scopeVerified && after.progressCleared;
            evidence[`${evidenceKey}ErrorFree`] = after.scopeVerified && after.errorFree;
            evidence[`${evidenceKey}ScopeVerified`] = nativeCover.scopeVerified === true && after.scopeVerified;
            evidence[`${evidenceKey}BaselineMediaCount`] = previewBefore.fingerprints.length;
            evidence[`${evidenceKey}PreviewMediaCount`] = after.fingerprints.length;
            evidence[`${evidenceKey}PreviewIncrement`] = items.length;
            evidence[`${evidenceKey}ExpectedItemCount`] = items.length;
            evidence[`${evidenceKey}CompletionStable`] = true;
          } else {
            await recordUploadState(evidenceKey, previewBefore, items);
          }
        }
        return;
      }
      if (rule.coverControl.requireExplicitUpload) {
        missing.push(name);
        return;
      }
    }
    if (rule.mediaUploadTriggerSelectors?.[kind]?.length) {
      const trigger = await firstVisibleEventually(page, rule.mediaUploadTriggerSelectors[kind], 3000);
      if (!trigger) {
        evidence[evidenceKey] = false;
        missing.push(name);
        return;
      }
      await trigger.click({ timeout: 1500 }).catch(() => {});
      await page.waitForTimeout(200);
    }
    let match = rule.preferMediaUploadTriggerKinds?.includes(kind)
      ? { locator: null, count: 0 }
      : await uniqueFileInput(page, kind);
    if (!match.locator && rule.mediaUploadTriggerNames?.[kind]?.length) {
      const chooserPromise = page.waitForEvent('filechooser', { timeout: 2500 }).catch(() => null);
      if (await clickExactTrigger(page, rule.mediaUploadTriggerNames[kind])) {
        const chooser = await chooserPromise;
        if (chooser) {
          try {
            const readObservedFiles = await observeFileSelectionBeforeNavigation(page, chooser.element());
            await chooser.setFiles(filesToUpload);
            const observed = await readObservedFiles();
            const selectionVerified = await recordFileSelection(
              evidenceKey,
              chooser.element(),
              items,
			  observed.files,
              expectedFiles,
            );
			evidence[`${evidenceKey}SelectionCapturedBeforeNavigation`] = observed.capturedBeforeNavigation;
            evidence[`${evidenceKey}ControlCount`] = 1;
            evidence[evidenceKey] = selectionVerified;
            await recordUploadState(evidenceKey, previewBefore, items);
            if (!selectionVerified) missing.push(name);
            return;
          } catch (_) {
            evidence[evidenceKey] = false;
            missing.push(name);
            return;
          }
        }
        const deadline = Date.now() + 5000;
        do {
          match = await uniqueFileInput(page, kind);
          if (match.locator) break;
          await page.waitForTimeout(500);
        } while (Date.now() < deadline);
      }
    }
    if (!match.locator) {
      const deadline = Date.now() + 5000;
      do {
        match = await uniqueFileInput(page, kind);
        if (match.locator) break;
        await page.waitForTimeout(250);
      } while (Date.now() < deadline);
    }
    evidence[`${evidenceKey}ControlCount`] = match.count;
    if (!match.locator) {
      missing.push(name);
      return;
    }
    try {
      const readObservedFiles = await observeFileSelectionBeforeNavigation(page, match.locator);
      await match.locator.setInputFiles(filesToUpload, { timeout: 3000 });
      const observed = await readObservedFiles();
      const selectionVerified = await recordFileSelection(
        evidenceKey,
        match.locator,
        items,
		  observed.files,
        expectedFiles,
      );
		evidence[`${evidenceKey}SelectionCapturedBeforeNavigation`] = observed.capturedBeforeNavigation;
      evidence[evidenceKey] = selectionVerified;
      await recordUploadState(evidenceKey, previewBefore, items);
      if (!selectionVerified) missing.push(name);
      if (name === 'media.article_images' && rule.requireArticleBodyImage) {
        const editor = await firstVisible(page, rule.body || []);
        const deadline = Date.now() + 5000;
        let inserted = false;
        do {
          inserted = Boolean(editor) && await editor.locator('img').count() > 0;
          if (inserted) break;
          await page.waitForTimeout(250);
        } while (Date.now() < deadline);
        evidence.contentImagesInserted = inserted;
        if (!inserted) {
          evidence[evidenceKey] = false;
          missing.push(name);
        }
      }
    } catch (_) {
      evidence[evidenceKey] = false;
      missing.push(name);
    }
  };
  if (input.content.type === 'video') {
    await upload('video', input.media.filter((item) => item.role === 'video'), 'media.video', 'videoUploaded');
    await upload('image', input.media.filter((item) => item.role === 'cover'), 'media.cover', 'coverUploaded');
  } else {
    const name = input.content.type === 'article' ? 'media.article_images' : 'media.content_images';
    await upload('image', input.media.filter((item) => item.role === 'content'), name, 'contentImagesUploaded');
    if (input.content.type === 'article') {
      const coverItems = input.media.filter((item) => item.role === 'cover');
      if (coverItems.length) {
        evidence.articleCoverSource = rule.articleCoverControl?.source || '';
        if (rule.articleCoverControl?.source === 'body_image') {
          await upload('image', coverItems, 'media.cover', 'articleCoverSourceUploaded');
        } else if (rule.articleCoverControl?.source === 'native_upload') {
          // The reviewed platform dialog is opened later; avoid treating that
          // native cover source as a missing generic article-image input.
          evidence.articleCoverSourceUploaded = false;
        } else {
          evidence.articleCoverSourceUploaded = false;
          missing.push('media.cover');
        }
      }
    }
  }
  return { evidence, missing };
}

function submitSelectorCandidates(rule) {
  const reviewed = [...(rule.submitSelectors || [])];
  if (rule.strictSubmitSelectors || rule.strictSubmitNames) return reviewed;
  return [...reviewed, ...(rule.submitNames || []).map((name) => `button:has-text("${name}")`)];
}

function requiredUploadEvidenceKeys(input) {
  if (input.content.type === 'video') {
    return [
      ...(input.media.some((item) => item.role === 'video') ? ['videoUploaded'] : []),
      ...(input.media.some((item) => item.role === 'cover') ? ['coverUploaded'] : []),
    ];
  }
  return input.media.some((item) => item.role === 'content') ? ['contentImagesUploaded'] : [];
}

function mediaItemsForUploadEvidence(input, key) {
  const role = key === 'videoUploaded' ? 'video' : key === 'coverUploaded' ? 'cover' : 'content';
  return orderedMediaItems(input.media.filter((item) => item.role === role));
}

function exactFileDescriptorSet(actual, expected) {
  return Array.isArray(actual) && Array.isArray(expected)
    && actual.length === expected.length
    && actual.every((file, index) => (
      file?.name === expected[index]?.name && file?.size === expected[index]?.size
    ));
}

function uploadCompletionVerified(input, preparedEvidence, rule = PLATFORM_RULES[input.platform]) {
  const checks = requiredUploadEvidenceKeys(input).map((key) => {
    const items = mediaItemsForUploadEvidence(input, key);
    const immutableRequired = rule.immutableMediaPayload === true;
    const expectedFiles = immutableRequired
      ? preparedEvidence[`${key}ExpectedFiles`]
      : frozenFileDescriptors(items);
    const frozenSHA256s = items.map((item) => item.sha256);
    const sha256Verified = !immutableRequired || (
      preparedEvidence[`${key}ImmutablePayloadRequired`] === true
      && preparedEvidence[`${key}SHA256Verified`] === true
      && Array.isArray(preparedEvidence[`${key}SHA256s`])
      && preparedEvidence[`${key}SHA256s`].length === frozenSHA256s.length
      && preparedEvidence[`${key}SHA256s`]
        .every((value, index) => value === frozenSHA256s[index])
    );
    const immutableDescriptorsValid = !immutableRequired || (
      Array.isArray(expectedFiles)
      && expectedFiles.length === items.length
      && expectedFiles.every((file, index) => (
        file?.name === path.basename(items[index].path)
        && Number.isInteger(file?.size)
        && file.size > 0
      ))
    );
    const selectionVerified = !rule.requireFileSelectionReadback || (
      immutableDescriptorsValid
      && expectedFiles !== null
      && preparedEvidence[`${key}SelectionVerified`] === true
      && exactFileDescriptorSet(preparedEvidence[`${key}SelectedFiles`], expectedFiles)
    );
    const expectedItemCount = items.length;
    const previewCountSatisfied = !rule.requirePreviewIncrementPerItem || (
      preparedEvidence[`${key}ExpectedItemCount`] === expectedItemCount
      && preparedEvidence[`${key}PreviewIncrement`] >= expectedItemCount
    );
    return {
      key,
      selected: preparedEvidence[key] === true,
      nativeFirstFrame: key === 'coverUploaded'
        && preparedEvidence.coverAppliedAs === 'native_first_frame'
        && (
          !rule.coverControl?.requireExplicitUpload
          || rule.coverControl?.allowNativeDefaultWhenExplicitUnavailable === true
        ),
      explicitCoverPreview: key === 'coverUploaded'
        && ['cover_upload_dialog', 'direct_file_chooser'].includes(preparedEvidence.coverAppliedAs)
        && rule.coverControl?.requireExplicitUpload === true,
      sha256Verified,
      selectionVerified,
      expectedItemCount,
      previewCountSatisfied,
      completionStable: !rule.uploadCompletionStableMs
        || preparedEvidence[`${key}CompletionStable`] === true,
      scopeVerified: preparedEvidence[`${key}ScopeVerified`] !== false,
      preview: preparedEvidence[`${key}PreviewObserved`] === true,
      progressCleared: preparedEvidence[`${key}ProgressCleared`] === true,
      errorFree: preparedEvidence[`${key}ErrorFree`] === true,
    };
  });
  return {
    ok: checks.every((check) => check.selected && (
      check.nativeFirstFrame
      || (
        check.selectionVerified
        && check.sha256Verified
        && check.previewCountSatisfied
        && check.completionStable
        && check.scopeVerified
        && check.preview
        && check.progressCleared
        && check.errorFree
      )
    )),
    checks,
  };
}

async function currentPreparedUploadCompletion(page, input, rule) {
  const required = requiredUploadEvidenceKeys(input).filter((key) => (
    key !== 'coverUploaded' || !rule.coverPreviewMayUnmountAfterAdvance
  ));
  if (required.length === 0) return { ok: true, checks: [] };

  const current = await stableReviewedUploadSnapshot(
    page,
    rule,
    Math.min(input.timeoutMs, Math.max(2000, (rule.uploadCompletionStableMs || 0) + 1000)),
  );
  const snapshot = current.snapshot;
  const nativeCover = await uniqueVisibleExactText(page, rule.coverControl?.nativeDefaultNames || []);
  const explicitCoverCommitted = await nativeVideoCoverPreviewCommitted(page, rule.coverControl);

  const checks = [];
  for (const key of required) {
    const items = mediaItemsForUploadEvidence(input, key);
    const expectedItemCount = items.length;
    const visibleFileNames = [];
    for (const item of items) {
      const name = path.basename(item.path);
      const matches = page.getByText(name, { exact: true });
      let visible = false;
      for (let index = 0; index < await matches.count(); index += 1) {
        if (await matches.nth(index).isVisible().catch(() => false)) {
          visible = true;
          break;
        }
      }
      if (visible) visibleFileNames.push(name);
    }

    const nativeFirstFrame = key === 'coverUploaded'
      && !nativeCover.ambiguous
      && Boolean(nativeCover.control)
      && !rule.coverControl?.requireExplicitUpload;
    const explicitCoverPreview = key === 'coverUploaded'
      && rule.coverControl?.requireExplicitUpload === true;
    const minimumPreviewCount = rule.requirePreviewIncrementPerItem ? expectedItemCount : 1;
    const currentVisible = nativeFirstFrame || (
      current.stable
      && snapshot.scopeVerified
      && snapshot.progressCleared
      && snapshot.errorFree
      && (
        explicitCoverPreview
          ? explicitCoverCommitted
          : snapshot.fingerprints.length >= minimumPreviewCount
      )
    );
    checks.push({
      key,
      selected: currentVisible,
      nativeFirstFrame,
      explicitCoverPreview,
      expectedItemCount,
      visibleFileNames,
      scopeVerified: snapshot.scopeVerified,
      preview: snapshot.fingerprints.length >= minimumPreviewCount,
      progressCleared: snapshot.progressCleared,
      errorFree: snapshot.errorFree,
      completionStable: current.stable,
      current: currentVisible,
    });
  }
  return {
    ok: current.stable
      && snapshot.scopeVerified
      && snapshot.progressCleared
      && snapshot.errorFree
      && checks.every((check) => check.current),
    checks,
  };
}

async function nativeVideoCoverPreviewCommitted(page, definition = {}) {
  for (const selector of definition.committedPreviewSelectors || []) {
    const candidates = page.locator(selector);
    let committed = null;
    for (let index = 0; index < await candidates.count(); index += 1) {
      const candidate = candidates.nth(index);
      if (!await candidate.isVisible().catch(() => false)) continue;
      const source = await candidate.evaluate((node) => {
        if (node instanceof HTMLCanvasElement) return node.width > 0 && node.height > 0;
        if (node instanceof HTMLImageElement) return Boolean(node.currentSrc || node.getAttribute('src'));
        const background = node.style.backgroundImage || getComputedStyle(node).backgroundImage;
        return Boolean(background && background !== 'none');
      }).catch(() => false);
      if (!source || committed) return false;
      committed = candidate;
    }
    if (committed) return true;
  }
  if (!definition.committedPreviewNames?.length) return false;
  const label = await uniqueVisibleExactText(page, definition.committedPreviewNames);
  if (label.ambiguous || !label.control) return false;
  let scope = label.control;
  const maximumLevels = definition.committedPreviewAncestorLevels || 0;
  for (let level = 0; level <= maximumLevels; level += 1) {
    const media = scope.locator('img[src],canvas');
    const count = Math.min(await media.count(), 20);
    for (let index = 0; index < count; index += 1) {
      const candidate = media.nth(index);
      if (!await candidate.isVisible().catch(() => false)) continue;
      const committed = await candidate.evaluate((node) => {
        if (node instanceof HTMLCanvasElement) return node.width > 0 && node.height > 0;
        return Boolean(node.currentSrc || node.getAttribute('src'));
      }).catch(() => false);
      if (committed) return true;
    }
    scope = scope.locator('..');
  }
  return false;
}

async function firstReviewedSubmit(page, rule, timeoutMs = 0) {
  const deadline = Date.now() + timeoutMs;
  do {
    if (rule.strictSubmitNames) {
      const exact = await uniqueVisibleNamedControl(page, rule.submitNames || [], ['button']);
      if (exact.ambiguous) return null;
      if (exact.control) return exact.control;
    }
    const reviewed = await firstVisible(page, submitSelectorCandidates(rule));
    if (reviewed) return reviewed;
    for (const name of rule.submitTextNames || []) {
      const matches = page.getByText(name, { exact: true });
      for (let index = 0; index < await matches.count(); index += 1) {
        const text = matches.nth(index);
        if (!await text.isVisible().catch(() => false)) continue;
        const interactive = text.locator(
          'xpath=ancestor-or-self::*[self::button or @role="button" or contains(@class,"publish")][1]',
        );
        if (await interactive.count() && await interactive.isVisible().catch(() => false)) {
          return interactive;
        }
        return text;
      }
    }
    if (Date.now() >= deadline) break;
    await page.waitForTimeout(250);
  } while (true);
  return null;
}

async function firstReadyReviewedSubmit(page, rule, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let submitButton = null;
  do {
    submitButton = await firstReviewedSubmit(page, rule, Math.min(1000, Math.max(0, deadline - Date.now()))) || submitButton;
    if (submitButton && await submitButton.isEnabled().catch(() => false)) return submitButton;
    if (Date.now() >= deadline) break;
    await page.waitForTimeout(500);
  } while (true);
  return submitButton;
}

async function dismissReviewedPostAdvanceDialogs(page, rule) {
  let dismissed = false;
  for (let pass = 0; pass < 4; pass += 1) {
    let button = null;
    for (const name of rule.postAdvanceDismissNames || []) {
      const candidates = page.getByRole('button', { name, exact: true });
      for (let index = 0; index < await candidates.count(); index += 1) {
        const candidate = candidates.nth(index);
        if (!await candidate.isVisible().catch(() => false)) continue;
        const inDialog = await candidate.evaluate((element) => Boolean(element.closest(
          '[role="dialog"], ytcp-dialog, tp-yt-paper-dialog, paper-dialog',
        ))).catch(() => false);
        if (inDialog) {
          button = candidate;
          break;
        }
      }
      if (button) break;
    }
    if (!button) break;
    await button.click({ timeout: 1500 }).catch(() => {});
    dismissed = true;
    await page.waitForTimeout(250);
  }
  return dismissed;
}

async function applySeparatePlatformTags(page, input, rule) {
  const applied = [];
  const missing = [];
  if (!input.content.tags.length) return { applied, missing: rule.tagsRequired ? ['content.tags'] : [] };
  let tagInput = await firstVisibleEventually(page, rule.tagInputSelectors || [], 250);
  if (!tagInput) {
    const tagTrigger = await clickNamed(page, rule.tagTriggerNames || []);
    if (!tagTrigger) return { applied, missing: ['content.tags'] };
    tagInput = await firstVisibleEventually(page, rule.tagInputSelectors || [], 3000);
  }
  if (!tagInput) return { applied, missing: ['content.tags'] };

  for (const rawTag of input.content.tags) {
    const tag = rawTag.trim().replace(/^#+/, '').trim();
    try {
      await tagInput.fill(tag, { timeout: 1500 });
      await tagInput.press('Enter', { timeout: 1500 });
    } catch (_) {
      missing.push('content.tags');
      break;
    }
    // A visible matching string can be a search candidate, not a selected
    // CSDN tag. The reviewed picker clears its search input only after it
    // accepts the Enter action, so do not report a candidate as selected.
    if ((await tagInput.inputValue().catch(() => tag)).trim()) {
      missing.push('content.tags');
      break;
    }
    if (!await hasSelectedTagChip(page, tag)) {
      missing.push('content.tags');
      break;
    }
  }
  if (!missing.length) applied.push('content.tags');
  if (rule.tagPickerDismissWithEscape) {
    await tagInput.press('Escape', { timeout: 1500 }).catch(() => {});
    await page.waitForTimeout(100);
  }
  if (rule.tagPickerDismissWithCloseControl) {
    // A CSDN tag picker sometimes ignores Escape. Only a visible dedicated
    // close control (label/title/one-character × or a close-named class) is
    // eligible; the bounded loop cannot reach a publish action.
    for (const selector of rule.tagPickerCloseSelectors || []) {
      const candidate = page.locator(selector);
      if (await candidate.count() !== 1 || !await candidate.isVisible().catch(() => false)) continue;
      if (await candidate.click({ timeout: 1500 }).then(() => true).catch(() => false)) {
        await page.waitForTimeout(100);
        break;
      }
    }
    for (let pass = 0; pass < 2; pass += 1) {
      const candidates = page.locator("button, [role='button'], [aria-label], [title], [class*='close' i]");
      let closed = false;
      const count = Math.min(await candidates.count(), 80);
      for (let index = 0; index < count; index += 1) {
        const candidate = candidates.nth(index);
        if (!await candidate.isVisible().catch(() => false)) continue;
        // Once Escape has dismissed the tag picker, the outer publish modal
        // still exposes its own close button. It is never a tag-picker close
        // action and must not be clicked while preparing a final submit.
        const inTagPicker = await candidate.evaluate((element) => Boolean(element.closest('.mark_selection_box'))).catch(() => false);
        if (!inTagPicker) continue;
        const marker = await candidate.evaluate((element) => [
          element.getAttribute('aria-label') || '', element.getAttribute('title') || '',
          element.textContent || '', element.className || '',
        ].join(' ').trim().toLowerCase()).catch(() => '');
        if (!(/(^|\s)(×|x)(\s|$)/.test(marker) || /关闭|close/.test(marker))) continue;
        if (await candidate.click({ timeout: 1500 }).then(() => true).catch(() => false)) {
          closed = true;
          await page.waitForTimeout(100);
          break;
        }
      }
      if (!closed) break;
    }
    // The tag picker is an overlay. A click handler can be accepted while the
    // overlay stays open, in which case a later final-button click would be
    // intercepted. Treat that as an unprepared form rather than submitting.
    const pickerStillOpen = await page.locator('.mark_selection_box').evaluateAll((nodes) => nodes.some((node) => {
      const style = window.getComputedStyle(node);
      const rect = node.getBoundingClientRect();
      return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
    })).catch(() => false);
    if (pickerStillOpen) missing.push('content.tags');
  }
  await clickNamed(page, rule.tagPickerDismissNames || []);
  return { applied, missing };
}

async function exactIntermediateSubmit(page, definition) {
  if (!definition) return { control: null, ambiguous: false, root: null };
  const rootMatch = await uniqueVisible(page, definition.rootSelectors || []);
  if (rootMatch.ambiguous || !rootMatch.control) {
    return { control: null, ambiguous: rootMatch.ambiguous, root: null };
  }
  const submitMatch = await uniqueVisible(rootMatch.control, definition.submitSelectors || []);
  if (submitMatch.ambiguous || !submitMatch.control) {
    return { control: null, ambiguous: submitMatch.ambiguous, root: rootMatch.control };
  }
  const expected = new Set((definition.submitNames || []).map(normalizedSettingText));
  if (expected.size
    && !expected.has(normalizedSettingText(await submitMatch.control.innerText().catch(() => '')))) {
    return { control: null, ambiguous: false, root: rootMatch.control };
  }
  return { control: submitMatch.control, ambiguous: false, root: rootMatch.control };
}

async function prepareIntermediateSubmit(page, definition, input) {
  const deadline = Date.now() + (definition.readyTimeoutMs || 3000);
  let exact = await exactIntermediateSubmit(page, definition);
  while (!exact.ambiguous && (!exact.root || !exact.control) && Date.now() < deadline) {
    await page.waitForTimeout(100);
    exact = await exactIntermediateSubmit(page, definition);
  }
  if (exact.ambiguous || !exact.root || !exact.control) {
    return { ready: false, settingsApplied: [], missing: ['publish.options_dialog'] };
  }
  for (const option of definition.booleanOptions || []) {
    const expected = option.expectedFromPublishAt ? Boolean(input?.publishAt) : option.expected;
    const label = await uniqueVisibleExactText(exact.root, [option.label], option.labelSelectors || []);
    if (label.ambiguous) {
      return { ready: false, settingsApplied: [], missing: [`publish.option.${option.label}`] };
    }
    if (!label.control && option.absentWhenExpectedFalse && expected === false) continue;
    if (!label.control) {
      return { ready: false, settingsApplied: [], missing: [`publish.option.${option.label}`] };
    }
    const scope = label.control.locator(
      'xpath=ancestor::div[contains(@class, "mass-send__td")][1]',
    );
    if (await scope.count() !== 1) {
      return { ready: false, settingsApplied: [], missing: [`publish.option.${option.label}`] };
    }
    const state = scope.locator("input[type='checkbox']");
    if (await state.count() !== 1) {
      return { ready: false, settingsApplied: [], missing: [`publish.option.${option.label}`] };
    }
    const checkbox = state.first();
    let optionReady = await booleanControlState(checkbox) === expected;
    if (!optionReady) {
      // WeChat visually covers its native checkbox with the switch box. A
      // direct setChecked waits on pointer interception even though the exact
      // reviewed label remains actionable. Force-clicking that label is still
      // a trusted DOM click and never moves the user's OS pointer.
      const switchLabel = checkbox.locator('xpath=ancestor::label[1]');
      if (await switchLabel.count() === 1) {
        await switchLabel.click({ force: true, timeout: 1500 }).catch(() => {});
        await page.waitForTimeout(100);
        optionReady = await booleanControlState(checkbox) === expected;
      }
      if (!optionReady) optionReady = await setBooleanControl(checkbox, expected);
    }
    if (!optionReady) {
      return { ready: false, settingsApplied: [], missing: [`publish.option.${option.label}`] };
    }
  }
  const refreshed = await exactIntermediateSubmit(page, definition);
  const ready = !refreshed.ambiguous
    && Boolean(refreshed.control)
    && await refreshed.control.isEnabled().catch(() => false);
  return {
    ready,
    settingsApplied: ready ? [...(definition.settingsApplied || [])] : [],
    missing: ready ? [] : ['publish.options_dialog'],
  };
}

async function prepareRequiredPlatformOptions(page, input, rule) {
  const applied = [];
  const missing = [];
  const settingsApplied = [];
  if (rule.separateTagControl && rule.tagsBeforeAdvance) {
    const tags = await applySeparatePlatformTags(page, input, rule);
    applied.push(...tags.applied);
    missing.push(...tags.missing);
  }

  const optionsDialogVisible = async () => {
    if (!rule.optionsDialogSelectors?.length) return true;
    const visible = await uniqueVisible(page, rule.optionsDialogSelectors);
    return !visible.ambiguous && Boolean(visible.control);
  };
  let finalButton = await firstReviewedSubmit(page, rule);
  if (!await optionsDialogVisible()) finalButton = null;
  const hasAdvance = Boolean(rule.advanceSelectors?.length || rule.advanceNames?.length);
  const advanceCount = rule.advanceCount || 0;
  if (hasAdvance && (advanceCount > 0 || !finalButton)) {
    const requiredClicks = advanceCount || 1;
    for (let index = 0; index < requiredClicks; index += 1) {
      let advance = await firstVisibleEventually(page, rule.advanceSelectors || [], 5000);
      if (!advance && rule.advanceNames?.length) {
        advance = await firstVisibleNamedControl(page, rule.advanceNames, ['button']);
      }
      if (!advance) return { applied, missing: [...missing, 'publish.options_trigger'], settingsApplied };
      try {
        await advance.click({ timeout: 1500 });
      } catch (_) {
        return { applied, missing: [...missing, 'publish.options_trigger'], settingsApplied };
      }
      await page.waitForTimeout(300);
    }
    if (await dismissReviewedPostAdvanceDialogs(page, rule)) {
      applied.push('publish.post_advance_dialog');
    }
    if (rule.intermediateSubmitControl) {
      const intermediate = await prepareIntermediateSubmit(page, rule.intermediateSubmitControl, input);
      if (intermediate.ready) applied.push('publish.options_dialog_open');
      settingsApplied.push(...intermediate.settingsApplied);
      return {
        applied,
        missing: [...missing, ...intermediate.missing],
        settingsApplied,
      };
    }
    finalButton = await firstReviewedSubmit(page, rule, 5000);
    for (let retry = 0; retry < (rule.advanceRetryAttempts || 0)
      && (!finalButton || !await optionsDialogVisible()); retry += 1) {
      const advance = await firstVisibleEventually(page, rule.advanceSelectors || [], 2000);
      if (!advance || !await advance.click({ timeout: 1500 }).then(() => true).catch(() => false)) break;
      await page.waitForTimeout(500);
      finalButton = await firstReviewedSubmit(page, rule, 3000);
    }
    if (!finalButton || !await optionsDialogVisible()) {
      return { applied, missing: [...missing, 'publish.options_dialog'], settingsApplied };
    }
  }
  if (hasAdvance && finalButton) applied.push('publish.final_review_open');

  if (rule.separateTagControl && !rule.tagsBeforeAdvance && !rule.articleCoverControl?.beforeSeparateTags) {
    const tags = await applySeparatePlatformTags(page, input, rule);
    applied.push(...tags.applied);
    missing.push(...tags.missing);
  }
  return { applied, missing, settingsApplied };
}

async function exactComposerSubmit(page, rule) {
  if (!rule.submitRootSelector) {
    const control = rule.submitReadyTimeoutMs
      ? await firstReadyReviewedSubmit(page, rule, rule.submitReadyTimeoutMs)
      : await firstReviewedSubmit(page, rule, 5000);
    return { control, ambiguous: false, root: page };
  }
  const resolved = await uniqueVisibleScope(page, rule.submitRootSelector);
  if (!resolved.verified) return { control: null, ambiguous: resolved.ambiguous, root: null };
  const matches = resolved.scope.locator((rule.submitSelectors || []).join(','));
  const count = await matches.count();
  if (count !== 1) return { control: null, ambiguous: count > 1, root: resolved.scope };
  const host = matches.first();
  if (!await host.isVisible().catch(() => false)) return { control: null, ambiguous: false, root: resolved.scope };
  if (rule.closedShadowSubmit) {
    // XHS keeps the actionable button in a closed shadow root. Backend node
    // identity ties AX review, click evidence, and the one trusted click.
    const inner = await exactClosedShadowSubmit(page, host, rule.submitNames || []);
    return { ...inner, root: resolved.scope };
  }
  const control = host;
  return { control, ambiguous: false, root: resolved.scope };
}

function nodeAttributes(node) {
  const attributes = new Map();
  for (let index = 0; index < (node?.attributes || []).length; index += 2) {
    attributes.set(String(node.attributes[index]).toLowerCase(), String(node.attributes[index + 1] ?? ''));
  }
  return attributes;
}

function piercedDescendants(nodes) {
  const descendants = [];
  const visit = (node) => {
    descendants.push(node);
    for (const child of node.children || []) visit(child);
    for (const shadow of node.shadowRoots || []) visit(shadow);
    if (node.contentDocument) visit(node.contentDocument);
  };
  for (const node of nodes || []) visit(node);
  return descendants;
}

function axBooleanProperty(node, name) {
  return node?.properties?.find((property) => property.name === name)?.value?.value === true;
}

function attributeIsTrue(attributes, name) {
  return attributes.has(name) && ['', 'true', name].includes(attributes.get(name).toLowerCase());
}

async function describeLocatorPierced(page, locator, session) {
  const marker = `__alphaNexusShadowHost_${process.pid}_${++clickEvidenceBindingSequence}`;
  let objectId = '';
  try {
    await locator.evaluate((node, key) => { globalThis[key] = node; }, marker);
    const remote = await session.send('Runtime.evaluate', {
      expression: `globalThis[${JSON.stringify(marker)}]`,
      returnByValue: false,
    });
    objectId = remote?.result?.objectId || '';
    if (!objectId) return null;
    const described = await session.send('DOM.describeNode', {
      objectId,
      depth: -1,
      pierce: true,
    });
    return described?.node || null;
  } finally {
    await locator.evaluate((_node, key) => { delete globalThis[key]; }, marker).catch(() => {});
    if (objectId) await session.send('Runtime.releaseObject', { objectId }).catch(() => {});
  }
}

function exactShadowButton(hostNode, axTree, names) {
  const shadowDescendants = piercedDescendants(hostNode?.shadowRoots || []);
  const domButtons = new Map(
    shadowDescendants
      .filter((node) => node.localName === 'button' && Number.isInteger(node.backendNodeId))
      .map((node) => [node.backendNodeId, node]),
  );
  const acceptedNames = new Set(names);
  const matches = new Map();
  for (const node of axTree?.nodes || []) {
    if (node.ignored || node.role?.value !== 'button' || !acceptedNames.has(String(node.name?.value || '').trim())) continue;
    if (!domButtons.has(node.backendDOMNodeId)) continue;
    matches.set(node.backendDOMNodeId, { dom: domButtons.get(node.backendDOMNodeId), ax: node });
  }
  if (matches.size !== 1) {
    return { match: null, ambiguous: matches.size > 1 };
  }
  return { match: [...matches.values()][0], ambiguous: false };
}

function shadowButtonReady(hostNode, match) {
  const hostAttributes = nodeAttributes(hostNode);
  const buttonAttributes = nodeAttributes(match.dom);
  const disabled = buttonAttributes.has('disabled')
    || attributeIsTrue(buttonAttributes, 'aria-disabled')
    || attributeIsTrue(hostAttributes, 'submit-disabled')
    || attributeIsTrue(hostAttributes, 'aria-disabled')
    || axBooleanProperty(match.ax, 'disabled');
  const loading = attributeIsTrue(buttonAttributes, 'aria-busy')
    || attributeIsTrue(hostAttributes, 'submit-loading')
    || attributeIsTrue(hostAttributes, 'aria-busy')
    || axBooleanProperty(match.ax, 'busy');
  return !disabled && !loading;
}

async function currentShadowButtonReady(session, hostBackendNodeId, buttonBackendNodeId, names) {
  const [described, axTree] = await Promise.all([
    session.send('DOM.describeNode', {
      backendNodeId: hostBackendNodeId,
      depth: -1,
      pierce: true,
    }),
    session.send('Accessibility.getFullAXTree'),
  ]);
  const exact = exactShadowButton(described?.node, axTree, names);
  return Boolean(
    !exact.ambiguous
    && exact.match
    && exact.match.dom.backendNodeId === buttonBackendNodeId
    && shadowButtonReady(described.node, exact.match),
  );
}

async function withPageCDPSession(page, operation) {
  const session = await page.context().newCDPSession(page);
  try {
    return await operation(session);
  } finally {
    await session.detach().catch(() => {});
  }
}

async function callOnBackendNode(session, backendNodeId, operation, argument) {
  const resolved = await session.send('DOM.resolveNode', { backendNodeId });
  const objectId = resolved?.object?.objectId || '';
  if (!objectId) throw new Error('XHS_SHADOW_SUBMIT_NODE_MISSING');
  try {
    const result = await session.send('Runtime.callFunctionOn', {
      objectId,
      functionDeclaration: `function(argument) { return (${operation.toString()})(this, argument); }`,
      arguments: [{ value: argument }],
      awaitPromise: true,
      returnByValue: true,
    });
    if (result.exceptionDetails) throw new Error('XHS_SHADOW_SUBMIT_EVALUATION_FAILED');
    return result.result?.value;
  } finally {
    await session.send('Runtime.releaseObject', { objectId }).catch(() => {});
  }
}

function closedShadowSubmitControl(page, hostBackendNodeId, buttonBackendNodeId, names) {
  return {
    evaluate: (operation, argument) => withPageCDPSession(
      page,
      (session) => callOnBackendNode(session, buttonBackendNodeId, operation, argument),
    ),
    isEnabled: () => withPageCDPSession(
      page,
      (session) => currentShadowButtonReady(session, hostBackendNodeId, buttonBackendNodeId, names),
    ).catch(() => false),
    click: (_options = {}) => withPageCDPSession(page, async (session) => {
      if (!await currentShadowButtonReady(session, hostBackendNodeId, buttonBackendNodeId, names)) {
        throw new Error('XHS_SHADOW_SUBMIT_NOT_READY');
      }
      await session.send('DOM.scrollIntoViewIfNeeded', { backendNodeId: buttonBackendNodeId });
      if (!await currentShadowButtonReady(session, hostBackendNodeId, buttonBackendNodeId, names)) {
        throw new Error('XHS_SHADOW_SUBMIT_NOT_READY');
      }
      const rect = await callOnBackendNode(
        session,
        buttonBackendNodeId,
        (node) => {
          const bounds = node.getBoundingClientRect();
          return {
            x: bounds.left + (bounds.width / 2),
            y: bounds.top + (bounds.height / 2),
            width: bounds.width,
            height: bounds.height,
          };
        },
      );
      if (!rect || rect.width <= 0 || rect.height <= 0 || !Number.isFinite(rect.x) || !Number.isFinite(rect.y)) {
        throw new Error('XHS_SHADOW_SUBMIT_NOT_VISIBLE');
      }
      await session.send('Input.dispatchMouseEvent', {
        type: 'mousePressed',
        x: rect.x,
        y: rect.y,
        button: 'left',
        buttons: 1,
        clickCount: 1,
      });
      await session.send('Input.dispatchMouseEvent', {
        type: 'mouseReleased',
        x: rect.x,
        y: rect.y,
        button: 'left',
        buttons: 0,
        clickCount: 1,
      });
    }),
  };
}

async function exactClosedShadowSubmit(page, host, names) {
  return withPageCDPSession(page, async (session) => {
    const hostNode = await describeLocatorPierced(page, host, session);
    if (!hostNode?.backendNodeId || !hostNode.shadowRoots?.some((root) => root.shadowRootType === 'closed')) {
      return { control: null, ambiguous: false };
    }
    const axTree = await session.send('Accessibility.getFullAXTree');
    const exact = exactShadowButton(hostNode, axTree, names);
    if (!exact.match) return { control: null, ambiguous: exact.ambiguous };
    return {
      control: closedShadowSubmitControl(
        page,
        hostNode.backendNodeId,
        exact.match.dom.backendNodeId,
        names,
      ),
      ambiguous: false,
    };
  }).catch(() => ({ control: null, ambiguous: false }));
}

async function firstReadyExactComposerSubmit(page, rule, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  do {
    const exact = await exactComposerSubmit(page, rule);
    if (exact.ambiguous) return null;
    if (exact.control && await exact.control.isEnabled().catch(() => false)) return exact.control;
    if (Date.now() >= deadline) break;
    await page.waitForTimeout(500);
  } while (true);
  return null;
}

async function locatorExactValue(locator, expected) {
  if (!locator) return false;
  const actual = await locator.evaluate((node) => (
    node.matches('input,textarea') ? node.value : node.innerText
  )).catch(() => null);
  return actual !== null && contentEditableValueMatches(actual, expected);
}

function expectedNativeTopics(tags, definition) {
  const expected = [...new Set(tags.map((tag) => tag.trim().replace(/^#+/, '').trim()).filter(Boolean))];
  if (definition.preferCJK) {
    expected.sort((left, right) => Number(!/[\u3400-\u9fff]/u.test(left)) - Number(!/[\u3400-\u9fff]/u.test(right)));
  }
  if (definition.maxTopics) expected.splice(definition.maxTopics);
  return expected;
}

async function currentUploadCompletion(page, input, rule, preparedEvidence) {
  const prepared = uploadCompletionVerified(input, preparedEvidence, rule);
  const current = await stableReviewedUploadSnapshot(
    page,
    rule,
    Math.min(input.timeoutMs, Math.max(2000, (rule.uploadCompletionStableMs || 0) + 1000)),
  );
  const snapshot = current.snapshot;
  let currentCoverPreview = false;
  if (prepared.checks.some((check) => check.explicitCoverPreview)) {
    const previewMatch = await uniqueVisible(page, rule.coverControl.previewSelectors || []);
    const background = !previewMatch.ambiguous
      ? await backgroundImageValue(previewMatch.control)
      : '';
    const before = preparedEvidence.coverUploadedPreviewBackgroundBefore;
    const after = preparedEvidence.coverUploadedPreviewBackgroundAfter;
    currentCoverPreview = Boolean(
      background
      && background !== 'none'
      && after
      && after !== before
      && background === after,
    );
    if (!currentCoverPreview) {
      currentCoverPreview = await nativeVideoCoverPreviewCommitted(page, rule.coverControl);
    }
  }
  const checks = prepared.checks.map((check) => {
    if (check.nativeFirstFrame) return { ...check, current: true };
    if (check.explicitCoverPreview) {
      const preparedPreviewCanPersistAcrossAdvance = rule.coverPreviewMayUnmountAfterAdvance === true
        && check.selected
        && check.selectionVerified
        && check.preview
        && check.completionStable
        && check.scopeVerified
        && check.progressCleared
        && check.errorFree;
      return {
        ...check,
        current: snapshot.scopeVerified && snapshot.progressCleared && snapshot.errorFree
          && (currentCoverPreview || preparedPreviewCanPersistAcrossAdvance),
      };
    }
    const baseline = preparedEvidence[`${check.key}BaselineMediaCount`];
    const preview = preparedEvidence[`${check.key}PreviewMediaCount`];
    const preparedFingerprints = preparedEvidence[`${check.key}PreviewFingerprints`];
    const countsValid = Number.isInteger(baseline) && Number.isInteger(preview)
      && preview > baseline
      && (!rule.requirePreviewIncrementPerItem || preview - baseline >= check.expectedItemCount)
      && snapshot.fingerprints.length >= preview;
    const fingerprintsStillPresent = Array.isArray(preparedFingerprints)
      && preparedFingerprints.length >= check.expectedItemCount
      && preparedFingerprints.every((fingerprint) => snapshot.fingerprints.includes(fingerprint));
    return {
      ...check,
      current: current.stable
        && snapshot.scopeVerified
        && snapshot.progressCleared
        && snapshot.errorFree
        && countsValid
        && fingerprintsStillPresent,
    };
  });
  return {
    ok: prepared.ok && current.stable
      && snapshot.scopeVerified && snapshot.progressCleared && snapshot.errorFree
      && checks.every((check) => check.current),
    checks,
  };
}

function xhsComposerModeMatches(page, input, rule) {
  const expected = rule.composerURLs?.[input.content.type];
  if (!expected) return false;
  try {
    const actualURL = new URL(page.url());
    const expectedURL = new URL(expected);
    if (actualURL.origin !== expectedURL.origin || actualURL.pathname !== expectedURL.pathname) return false;
    for (const [key, value] of expectedURL.searchParams) {
      const actualValues = actualURL.searchParams.getAll(key);
      if (actualValues.length !== 1 || actualValues[0] !== value) return false;
    }
    return true;
  } catch (_) {
    return false;
  }
}

function requiredSubmitURLPrefixes(input, rule) {
  const entryPrefixes = rule.entryFlows?.[input.content.type]?.requiredURLPrefixes;
  if (entryPrefixes?.length) return entryPrefixes;
  const typedComposerPrefixes = rule.composerURLPrefixes?.[input.content.type] || [];
  if (typedComposerPrefixes.length) {
    return [...new Set([rule.composerUrl, ...typedComposerPrefixes].filter(Boolean))];
  }
  if (rule.requiredURLPrefixes?.length) return rule.requiredURLPrefixes;
  return rule.composerUrl ? [rule.composerUrl] : [];
}

function douyinComposerModeMatches(page, input, rule) {
  return requiredSubmitURLPrefixes(input, rule).some((prefix) => page.url().startsWith(prefix));
}

async function exactEnumChoiceSet(root, definition, expectedValue) {
  for (const [value, option] of Object.entries(definition.options || {})) {
    let match = await uniqueVisible(root, option.selectors || []);
    if (match.ambiguous) return false;
    if (!match.control) {
      match = await uniqueVisibleNamedControl(root, option.names || [], ['radio', 'checkbox']);
      if (match.ambiguous) return false;
    }
    if (!match.control) return false;
    const expected = value === String(expectedValue);
    if (await choiceControlState(match.control) !== expected) return false;
  }
  return true;
}

async function douyinNativeTopicSet(locator, definition) {
  if (!locator || !definition.nativeTopicSelectors?.length) return { topics: [], invalid: true };
  return locator.locator(definition.nativeTopicSelectors.join(',')).evaluateAll((nodes) => {
    const topics = [];
    let invalid = false;
    for (const node of nodes) {
      const text = String(node.innerText || node.textContent || '')
        .replace(/[\u200b\uFEFF]/gu, '')
        .trim();
      if (!text.startsWith('#') || !text.slice(1).trim()) invalid = true;
      else topics.push(text.slice(1).trim());
    }
    return { topics, invalid };
  }).catch(() => ({ topics: [], invalid: true }));
}

async function douyinScheduleState(root, input, rule, preparedEvidence = {}) {
  if (!input.publishAt || !await exactScheduleChoiceChecked(root, rule.scheduleControl)) return false;
  const immediate = await uniqueVisibleNamedControl(
    root,
    rule.scheduleControl.immediateNames || [],
    ['radio', 'checkbox'],
  );
  if (immediate.ambiguous
    || !immediate.control
    || await scheduleChoiceState(immediate.control) !== false) return false;
  const match = await uniqueVisible(root, rule.scheduleControl.dateTimeInputs || []);
  if (match.ambiguous || !match.control) return false;
  const resolved = typeof preparedEvidence.publishTimeValue === 'string'
    && /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/.test(preparedEvidence.publishTimeValue)
    ? preparedEvidence.publishTimeValue
    : localPublishTimeParts(input.publishAt).dateTimeMinute;
  return await match.control.inputValue().catch(() => null) === resolved;
}

async function revalidateDouyinGraphTextBeforeSubmit(page, input, rule, preparedEvidence) {
  const upload = await currentUploadCompletion(page, input, rule, preparedEvidence);
  const titleMatch = await uniqueVisible(page, ["input[placeholder='添加作品标题']"]);
  const bodyMatch = await uniqueVisible(page, ["div[contenteditable='true']"]);
  const expectedBody = bodyWithoutFrozenTrailingTags(input.content, rule);
  const expectedTopics = expectedNativeTopics(input.content.tags, rule.topicControl);
  const nativeTopics = bodyMatch.control
    ? await douyinNativeTopicSet(bodyMatch.control, rule.topicControl)
    : { topics: [], invalid: true };
  const ai = await enumSelectedTextState(
    page,
    rule.settingControls.self_declaration.options.ai,
  );
  const result = {
    upload: upload.ok,
    composerMode: douyinComposerModeMatches(page, input, rule),
    title: !titleMatch.ambiguous
      && await locatorExactValue(titleMatch.control, input.content.title),
    body: !bodyMatch.ambiguous
      && await exactBodyWithoutNativeTopics(bodyMatch.control, rule.topicControl, expectedBody),
    topics: !nativeTopics.invalid && exactTopicSetMatches(nativeTopics.topics, expectedTopics),
    visibilityPublic: input.settings.visibility === 'public'
      && await exactEnumChoiceSet(page, rule.settingControls.visibility, 'public'),
    saveDisabled: input.settings.allow_download === false
      && await exactEnumChoiceSet(page, rule.settingControls.allow_download, false),
    aiDeclaration: input.settings.self_declaration === 'ai'
      && !ai.ambiguous && ai.selected,
    schedule: await douyinScheduleState(page, input, rule, preparedEvidence),
    collectionEmpty: input.settings.collection === ''
      && await emptyTextSettingReadback(page, rule.settingControls.collection),
    hotspotEmpty: input.settings.hotspot === ''
      && await emptyTextSettingReadback(page, rule.settingControls.hotspot),
    musicEmpty: input.settings.music === ''
      && await emptyTextSettingReadback(page, rule.settingControls.music),
    commentNotPresent: input.settings.comment_enabled === true
      && await verifiedAbsentBooleanSetting(
        page,
        rule,
        rule.settingControls.comment_enabled,
        true,
        'graph_text',
      ),
  };
  return {
    ok: Object.values(result).every(Boolean),
    evidence: result,
    uploadChecks: upload.checks,
  };
}

async function xhsFinalBooleanState(page, root, rule, key, expected) {
  const definition = rule.settingControls[key];
  let match = definition.scopedLabel
    ? await uniqueScopedBooleanControl(root, definition)
    : await uniqueVisible(root, definition.selectors || []);
  if (match.ambiguous) return false;
  let control = match.control;
  if (!control && !definition.scopedLabel) {
    match = await uniqueVisibleNamedControl(root, definition.names, ['switch', 'checkbox']);
    if (match.ambiguous) return false;
    control = match.control;
  }
  if (control) return await booleanControlState(control) === expected;
  return verifiedAbsentBooleanSetting(page, rule, definition, expected);
}

async function xhsEmptySettingState(root, definition) {
  return emptyTextSettingReadback(root, definition);
}

async function xhsImmediatePublishState(root, input, rule) {
  if (input.publishAt || !rule.scheduleControl?.scopedLabel) return false;
  const match = await uniqueScopedBooleanControl(root, rule.scheduleControl);
  if (match.ambiguous || !match.control) return false;
  return await booleanControlState(match.control) === false;
}

async function revalidateXHSBeforeSubmit(page, root, input, rule, preparedEvidence) {
  const titleMatch = await uniqueVisible(root, rule.title || []);
  const bodyMatch = await uniqueVisible(root, rule.body || []);
  const expectedBody = bodyWithoutFrozenTrailingTags(input.content, rule);
  const title = !titleMatch.ambiguous
    && await locatorExactValue(titleMatch.control, input.content.title);
  const body = !bodyMatch.ambiguous
    && await exactBodyWithoutNativeTopics(bodyMatch.control, rule.topicControl, expectedBody);
  const expectedTopics = expectedNativeTopics(input.content.tags, rule.topicControl);
  const native = bodyMatch.control
    ? await nativeTopicSet(bodyMatch.control, rule.topicControl)
    : { topics: [], invalid: true };
  const topics = !native.invalid && exactTopicSetMatches(native.topics, expectedTopics);
  const upload = await currentUploadCompletion(page, input, rule, preparedEvidence);
  const result = {
    title,
    body,
    topics,
    composerMode: xhsComposerModeMatches(page, input, rule),
    immediatePublish: await xhsImmediatePublishState(root, input, rule),
    upload: upload.ok,
  };
  if (Object.hasOwn(input.settings, 'content_source')) {
    const aiState = input.settings.content_source === 'ai'
      ? await enumSelectedTextState(root, rule.settingControls.content_source.options.ai)
      : { selected: false, ambiguous: false };
    result.ai = !aiState.ambiguous && aiState.selected;
  }
  if (Object.hasOwn(input.settings, 'visibility')) {
    const visibilityState = input.settings.visibility === 'public'
      ? await enumSelectedTextState(root, rule.settingControls.visibility.options.public)
      : { selected: false, ambiguous: false };
    result.visibility = !visibilityState.ambiguous && visibilityState.selected;
  }
  for (const [evidenceKey, settingKey, requiredValue] of [
    ['commentEnabled', 'comment_enabled', true],
    ['declareOriginal', 'declare_original', false],
    ['allowDuet', 'allow_duet', false],
    ['allowCopy', 'allow_copy', false],
  ]) {
    if (!Object.hasOwn(input.settings, settingKey)) continue;
    result[evidenceKey] = input.settings[settingKey] === requiredValue
      && await xhsFinalBooleanState(page, root, rule, settingKey, requiredValue);
  }
  for (const [evidenceKey, settingKey] of [
    ['locationEmpty', 'location'],
    ['collectionEmpty', 'collection'],
    ['groupEmpty', 'group'],
    ['relatedMediaEmpty', 'related_media'],
  ]) {
    if (!Object.hasOwn(input.settings, settingKey)) continue;
    result[evidenceKey] = input.settings[settingKey] === ''
      && await xhsEmptySettingState(root, rule.settingControls[settingKey]);
  }
  return { ok: Object.values(result).every(Boolean), evidence: result, uploadChecks: upload.checks };
}

async function pageTargetID(page) {
  let session = null;
  try {
    session = await page.context().newCDPSession(page);
    const result = await session.send('Target.getTargetInfo');
    const targetID = String(result?.targetInfo?.targetId || '');
    return /^[a-f0-9]{32}$/i.test(targetID) ? targetID : '';
  } catch (_) {
    return '';
  } finally {
    if (session) await session.detach().catch(() => {});
  }
}

async function clickReviewedSubmitOnce(page, control, evidence) {
  const bindingName = `__alphaNexusSubmitClick_${process.pid}_${++clickEvidenceBindingSequence}`;
  let observed = false;
  let acceptingEvidence = true;
  let resolveObserved;
  const observedPromise = new Promise((resolve) => { resolveObserved = resolve; });
  try {
    await page.exposeBinding(bindingName, (_source, clickedAt) => {
      if (observed || !acceptingEvidence) return;
      observed = true;
      evidence.submitClicked = true;
      evidence.submitClickedAt = Number.isFinite(Date.parse(clickedAt))
        ? new Date(clickedAt).toISOString()
        : new Date().toISOString();
      resolveObserved();
    });
    await control.evaluate((node, name) => {
      node.addEventListener('click', (event) => {
        if (!event.isTrusted) return;
        const notify = globalThis[name];
        if (typeof notify === 'function') {
          Promise.resolve(notify(new Date().toISOString())).catch(() => {});
        }
      }, { capture: true, once: true });
    }, bindingName);
  } catch (_) {
    evidence.submitOutcome = 'unverified';
    return false;
  }

  evidence.submitAttempted = true;
  let completed = false;
  try {
    await control.click({ noWaitAfter: true });
    completed = true;
  } catch (_) {
    // A navigation or page close can reject after the browser already sent
    // the trusted click. The capture binding above remains authoritative.
  }
  await Promise.race([
    observedPromise,
    new Promise((resolve) => setTimeout(resolve, 1000)),
  ]);
  acceptingEvidence = false;
  evidence.submitOutcome = completed && observed ? 'dispatched' : 'unknown';
  return completed && observed;
}

async function completePostSubmitConfirmation(page, definition) {
  if (!definition) return { ok: true, handled: false };
  const deadline = Date.now() + (definition.timeoutMs || 3000);
  let rootMatch = { control: null, ambiguous: false };
  do {
    rootMatch = await uniqueVisible(page, definition.rootSelectors || []);
    if (rootMatch.control || rootMatch.ambiguous || Date.now() >= deadline) break;
    await page.waitForTimeout(100);
  } while (true);
  if (rootMatch.ambiguous) return { ok: false, handled: true, ambiguous: true };
  if (!rootMatch.control) return { ok: true, handled: false };

  const root = rootMatch.control;
  let noDeclarationSelected = false;
  const declaration = await uniqueVisibleNamedControl(
    root,
    definition.noDeclarationNames || [],
    ['radio', 'checkbox'],
  );
  if (declaration.ambiguous) return { ok: false, handled: true, ambiguous: true };
  if (declaration.control) {
    let selected = await booleanControlState(declaration.control);
    if (!selected) {
      if (!await declaration.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) {
        return { ok: false, handled: true };
      }
      await page.waitForTimeout(100);
      selected = await booleanControlState(declaration.control);
    }
    if (!selected) return { ok: false, handled: true };
    noDeclarationSelected = true;
  }

  const final = await uniqueVisibleNamedControl(root, definition.finalNames || [], ['button']);
  if (final.ambiguous || !final.control || !await final.control.isEnabled().catch(() => false)) {
    return { ok: false, handled: true, ambiguous: final.ambiguous, noDeclarationSelected };
  }
  const clickEvidence = {};
  const clicked = await clickReviewedSubmitOnce(page, final.control, clickEvidence);
  return {
    ok: clicked,
    handled: true,
    noDeclarationSelected,
    confirmationSubmitAttempted: Boolean(clickEvidence.submitAttempted),
    confirmationSubmitClicked: Boolean(clickEvidence.submitClicked),
    confirmationSubmitClickedAt: clickEvidence.submitClickedAt,
    confirmationSubmitOutcome: clickEvidence.submitOutcome || 'unknown',
  };
}

async function postSubmitVisibleSignals(page) {
  const signals = [];
  const scopes = [page, ...page.frames().filter((frame) => frame !== page.mainFrame())];
  for (const scope of scopes) {
    const candidates = scope.locator(
      "[role='alert'],[role='dialog'],.weui-desktop-toast,.weui-desktop-dialog",
    );
    const count = Math.min(await candidates.count().catch(() => 0), 20);
    for (let index = 0; index < count; index += 1) {
      const candidate = candidates.nth(index);
      if (!await candidate.isVisible().catch(() => false)) continue;
      const text = normalizedSettingText(await candidate.innerText().catch(() => '')).slice(0, 300);
      if (text && !signals.includes(text)) signals.push(text);
    }
  }
  return signals.slice(0, 10);
}

async function visiblePlatformVerificationSignal(page, rule = {}) {
  const platformTexts = [...new Set(rule.platformVerificationTexts || [])];
  const scopes = [page, ...page.frames().filter((frame) => frame !== page.mainFrame())];
  for (const scope of scopes) {
    for (const text of platformTexts) {
      const signal = scope.getByText(text, { exact: false }).first();
      if (await signal.count().catch(() => 0) && await signal.isVisible().catch(() => false)) return text;
    }
    const verificationDialog = scope.locator("[role='dialog'],[role='alert']");
    for (const text of COMMON_PLATFORM_VERIFICATION_TEXTS) {
      const signal = verificationDialog.filter({ hasText: text }).first();
      if (await signal.count().catch(() => 0) && await signal.isVisible().catch(() => false)) return text;
    }
    for (const selector of COMMON_PLATFORM_VERIFICATION_SELECTORS) {
      const signal = scope.locator(selector).first();
      if (await signal.count().catch(() => 0) && await signal.isVisible().catch(() => false)) {
        return '平台安全验证控件';
      }
    }
  }
  return '';
}

function platformVerificationFailure(evidence, signal) {
  return {
    status: 'manual_takeover',
    step: 'platform_verification_required',
    errorCode: 'PLATFORM_VERIFICATION_REQUIRED',
    evidence: { ...evidence, platformVerificationSignal: signal },
  };
}

async function settleYouTubePostSubmitReminder(page) {
  const reminders = page.locator("[role='dialog']");
  const visibleReminders = [];
  for (let index = 0; index < await reminders.count().catch(() => 0); index += 1) {
    const candidate = reminders.nth(index);
    if (!await candidate.isVisible().catch(() => false)) continue;
    const text = normalizedSettingText(await candidate.innerText().catch(() => ''));
    if ([
      'We’re still checking your content',
      "We're still checking your content",
      '我们仍在检查你的内容',
    ].some((signal) => text.includes(signal))) visibleReminders.push(candidate);
  }
  if (visibleReminders.length !== 1) return { handled: false };
  const pageText = normalizedSettingText(await page.locator('body').innerText().catch(() => ''));
  if (!pageText.includes('Checks complete. No issues found.')
    && !(pageText.includes('检查完毕') && pageText.includes('未发现问题'))) return { handled: false };
  const acknowledgement = await uniqueVisibleNamedControl(
    visibleReminders[0],
    ['Got it', '知道了'],
    ['button'],
  );
  if (acknowledgement.ambiguous || !acknowledgement.control) return { handled: false };
  if (!await acknowledgement.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) {
    return { handled: false };
  }
  const deadline = Date.now() + 5000;
  do {
    for (const text of ['Video scheduled', '视频已安排']) {
      const scheduled = page.getByText(text, { exact: false }).first();
      if (await scheduled.isVisible().catch(() => false)) {
        return { handled: true, acknowledged: true, signal: 'Video scheduled' };
      }
    }
    if (Date.now() >= deadline) break;
    await page.waitForTimeout(100);
  } while (true);
  return { handled: true, acknowledged: true, signal: '' };
}

async function submitPreparedPage(page, input, preparedEvidence = {}, ruleOverride = null) {
  const rule = ruleOverride || PLATFORM_RULES[input.platform];
  const requiredURLPrefixes = requiredSubmitURLPrefixes(input, rule);
  if (requiredURLPrefixes.length && !requiredURLPrefixes.some((prefix) => page.url().startsWith(prefix))) {
    return preparedPageMissing(page, input.platform);
  }
  const existingVerification = await visiblePlatformVerificationSignal(page, rule);
  if (existingVerification) return platformVerificationFailure({}, existingVerification);
  let submitScope = page;
  // The WeChat Video editor fields hydrate inside a reviewed iframe, but its
  // single final “发表” control belongs to the outer composer page.
  // This is called only after the same approved action has prepared and
  // verified the form. It must not repair or rewrite fields here: a failed
  // preparation remains manual takeover and never reaches the final control.
  let exactSubmit = rule.intermediateSubmitControl
    ? await exactIntermediateSubmit(submitScope, rule.intermediateSubmitControl)
    : await exactComposerSubmit(submitScope, rule);
  let reopenedIntermediate = false;
  let reopenedPublishTime = null;
  if (rule.reopenIntermediateAtSubmit
    && rule.intermediateSubmitControl
    && !exactSubmit.ambiguous
    && !exactSubmit.control) {
    const reopened = await prepareRequiredPlatformOptions(page, input, rule);
    if (!reopened.missing.length) {
      reopenedPublishTime = await applyPublishTime(page, input, rule);
      if (!reopenedPublishTime.unverified.length) {
        exactSubmit = await exactIntermediateSubmit(submitScope, rule.intermediateSubmitControl);
        reopenedIntermediate = Boolean(exactSubmit.control) && !exactSubmit.ambiguous;
      }
    }
  }
  const submitButton = exactSubmit.control;
  const submitEnabled = Boolean(submitButton) && await submitButton.isEnabled().catch(() => false);
  const evidence = {
    submitFound: Boolean(submitButton),
    submitEnabled,
    submitAttempted: false,
    submitClicked: false,
    intermediateReopened: reopenedIntermediate,
    reopenedPublishTimeValue: reopenedPublishTime?.value || '',
  };
  if (exactSubmit.ambiguous) {
    return { status: 'manual_takeover', step: 'submit_button_ambiguous', errorCode: 'FORM_REVIEW_REQUIRED', evidence };
  }
  if (!submitButton) return { status: 'manual_takeover', step: 'submit_button_missing', errorCode: 'SUBMIT_BUTTON_NOT_FOUND', evidence };
  if (!submitEnabled) return { status: 'manual_takeover', step: 'submit_button_disabled', errorCode: 'FORM_REVIEW_REQUIRED', evidence };
  // CSDN stores its cover only in the final options panel. The submit-only
  // path must re-read that panel so an interrupted prior run cannot turn a
  // stale editor into a blind final click.
  if (input.platform === 'csdn' && input.media.some((item) => item.role === 'cover')) {
    const coverCommitted = await nativeArticleCoverPreviewCommitted(page, rule.articleCoverControl, 500);
    evidence.articleCoverFinalPreviewCommitted = coverCommitted;
    if (!coverCommitted) {
      return { status: 'manual_takeover', step: 'required_control_missing', errorCode: 'FORM_REVIEW_REQUIRED', evidence };
    }
  }
  // submit_prepared runs in a fresh worker process. The frozen DB target and
  // same live browser bind it to the reviewed prepare job; re-read the current
  // composer here because process-local upload evidence is intentionally gone.
  const uploadCompletion = input.action === 'submit_prepared'
    ? await currentPreparedUploadCompletion(page, input, rule)
    : uploadCompletionVerified(input, preparedEvidence, rule);
  evidence.uploadCompletion = uploadCompletion.checks;
  if (!uploadCompletion.ok) {
    return {
      status: 'manual_takeover',
      step: 'upload_completion_unverified',
      errorCode: 'MEDIA_UPLOAD_UNVERIFIED',
      evidence,
    };
  }
  if (input.platform === 'xhs') {
    const critical = await revalidateXHSBeforeSubmit(page, exactSubmit.root, input, rule, preparedEvidence);
    evidence.criticalRevalidation = critical.evidence;
    evidence.uploadCompletion = critical.uploadChecks;
    if (!critical.ok) {
      return {
        status: 'manual_takeover',
        step: 'critical_revalidation_failed',
        errorCode: 'FORM_REVIEW_REQUIRED',
        evidence,
      };
    }
  }
  if (input.platform === 'douyin' && input.content.type === 'graph_text') {
    const critical = await revalidateDouyinGraphTextBeforeSubmit(page, input, rule, preparedEvidence);
    evidence.criticalRevalidation = critical.evidence;
    evidence.uploadCompletion = critical.uploadChecks;
    if (!critical.ok) {
      return {
        status: 'manual_takeover',
        step: 'critical_revalidation_failed',
        errorCode: 'FORM_REVIEW_REQUIRED',
        evidence,
      };
    }
  }
  let finalSubmit;
  if (rule.intermediateSubmitControl) {
    const intermediateTargetID = await pageTargetID(page);
    const intermediateEvidence = {};
    if (!intermediateTargetID
      || !await clickReviewedSubmitOnce(page, exactSubmit.control, intermediateEvidence)) {
      Object.assign(evidence, {
        submitAttempted: Boolean(intermediateEvidence.submitAttempted),
        submitClicked: Boolean(intermediateEvidence.submitClicked),
        submitClickedAt: intermediateEvidence.submitClickedAt,
        submitOutcome: intermediateEvidence.submitOutcome || 'unknown',
        intermediateSubmitAttempted: Boolean(intermediateEvidence.submitAttempted),
        intermediateSubmitClicked: Boolean(intermediateEvidence.submitClicked),
      });
      return {
        status: evidence.submitAttempted ? 'awaiting_review' : 'manual_takeover',
        step: evidence.submitAttempted ? 'submission_unknown' : 'submit_target_unverified',
        errorCode: evidence.submitAttempted ? 'PUBLISH_CLICK_OUTCOME_UNKNOWN' : 'FORM_REVIEW_REQUIRED',
        evidence,
      };
    }
    Object.assign(evidence, {
      submitAttempted: true,
      submitClicked: true,
      submitClickedAt: intermediateEvidence.submitClickedAt,
      submitOutcome: intermediateEvidence.submitOutcome,
      intermediateSubmitAttempted: true,
      intermediateSubmitClicked: true,
    });
    const deadline = Date.now() + (rule.intermediateSubmitControl.finalTimeoutMs || 5000);
    do {
      finalSubmit = await exactComposerSubmit(submitScope, rule);
      if (finalSubmit.control || finalSubmit.ambiguous || Date.now() >= deadline) break;
      await page.waitForTimeout(100);
    } while (true);
    if (!finalSubmit.control || finalSubmit.ambiguous
      || await pageTargetID(page) !== intermediateTargetID) {
      return {
        status: 'awaiting_review',
        step: 'submission_unknown',
        errorCode: 'PUBLISH_CLICK_OUTCOME_UNKNOWN',
        evidence,
      };
    }
  } else {
    finalSubmit = await exactComposerSubmit(submitScope, rule);
  }
  if (finalSubmit.ambiguous) {
    return { status: 'manual_takeover', step: 'submit_button_ambiguous', errorCode: 'FORM_REVIEW_REQUIRED', evidence };
  }
  if (!finalSubmit.control || !await finalSubmit.control.isEnabled().catch(() => false)) {
    return { status: 'manual_takeover', step: 'submit_button_disabled', errorCode: 'FORM_REVIEW_REQUIRED', evidence };
  }
  const targetID = await pageTargetID(page);
  if (!targetID) {
    return { status: 'manual_takeover', step: 'submit_target_unverified', errorCode: 'FORM_REVIEW_REQUIRED', evidence };
  }
  evidence.submitTargetId = targetID;
  if (input.platform === 'douyin' && input.content.type === 'graph_text') {
    const finalUpload = await currentUploadCompletion(page, input, rule, preparedEvidence);
    evidence.finalUploadRevalidation = finalUpload.checks;
    if (!finalUpload.ok) {
      if (evidence.criticalRevalidation) evidence.criticalRevalidation.upload = false;
      return {
        status: 'manual_takeover',
        step: 'critical_revalidation_failed',
        errorCode: 'MEDIA_UPLOAD_UNVERIFIED',
        evidence,
      };
    }
    finalSubmit = await exactComposerSubmit(submitScope, rule);
    const finalTargetID = await pageTargetID(page);
    if (finalSubmit.ambiguous
      || !finalSubmit.control
      || !await finalSubmit.control.isEnabled().catch(() => false)
      || finalTargetID !== targetID) {
      return {
        status: 'manual_takeover',
        step: 'critical_revalidation_failed',
        errorCode: 'FORM_REVIEW_REQUIRED',
        evidence,
      };
    }
  }
  if (!await clickReviewedSubmitOnce(page, finalSubmit.control, evidence)) {
    if (!evidence.submitAttempted) {
      return {
        status: 'manual_takeover',
        step: 'submit_click_evidence_unavailable',
        errorCode: 'FORM_REVIEW_REQUIRED',
        evidence,
      };
    }
    return {
      status: 'awaiting_review',
      step: 'submission_unknown',
      errorCode: 'PUBLISH_CLICK_OUTCOME_UNKNOWN',
      evidence,
    };
  }
  if (!page.isClosed()) {
    await page.waitForTimeout(300).catch(() => {});
    if (!page.isClosed()) {
      if (input.platform === 'youtube') {
        const reminder = await settleYouTubePostSubmitReminder(page);
        if (reminder.handled) {
          evidence.postSubmitReminderAcknowledged = Boolean(reminder.acknowledged);
          if (reminder.signal) {
            return {
              status: 'awaiting_review',
              step: 'creator_backend_verification',
              errorCode: '',
              evidence: { ...evidence, platformSubmissionSignal: reminder.signal },
            };
          }
          return {
            status: 'awaiting_review',
            step: 'submission_unknown',
            errorCode: 'PUBLISH_CLICK_OUTCOME_UNKNOWN',
            evidence,
          };
        }
      }
      const immediateVerification = await visiblePlatformVerificationSignal(page, rule);
      if (immediateVerification) return platformVerificationFailure(evidence, immediateVerification);
    }
  }
  const confirmation = await completePostSubmitConfirmation(page, rule.postSubmitConfirmation);
  if (confirmation.handled) {
    Object.assign(evidence, {
      postSubmitConfirmation: true,
      noDeclarationSelected: Boolean(confirmation.noDeclarationSelected),
      confirmationSubmitAttempted: Boolean(confirmation.confirmationSubmitAttempted),
      confirmationSubmitClicked: Boolean(confirmation.confirmationSubmitClicked),
      confirmationSubmitClickedAt: confirmation.confirmationSubmitClickedAt,
      confirmationSubmitOutcome: confirmation.confirmationSubmitOutcome || 'unknown',
    });
  }
  if (!confirmation.ok) {
    const confirmationVerification = await visiblePlatformVerificationSignal(page, rule);
    if (confirmationVerification) return platformVerificationFailure(evidence, confirmationVerification);
    return {
      status: 'awaiting_review',
      step: 'submission_unknown',
      errorCode: 'PUBLISH_CLICK_OUTCOME_UNKNOWN',
      evidence,
    };
  }
  try {
    await page.waitForTimeout(1500);
    if (page.isClosed()) throw new Error('POST_CLICK_PAGE_CLOSED');
    if (rule.capturePostSubmitEvidence) {
      evidence.postClickSignals = await postSubmitVisibleSignals(page);
      const screenshotPath = await captureFailureScreenshot(page, input.platform);
      if (screenshotPath) evidence.postClickScreenshotPath = screenshotPath;
    }
    const verification = await visiblePlatformVerificationSignal(page, rule);
    if (verification) return platformVerificationFailure(evidence, verification);
    for (const text of rule.successText) {
      const signal = submitScope.getByText(text, { exact: false }).first();
      if (await signal.count() && await signal.isVisible().catch(() => false)) {
        return {
          status: 'awaiting_review',
          step: 'creator_backend_verification',
          errorCode: '',
          evidence: { ...evidence, platformSubmissionSignal: text },
        };
      }
    }
  } catch (_) {
    evidence.postClickObservation = 'unavailable';
  }
  if (rule.requirePostSubmitSignal) {
    return {
      status: 'awaiting_review',
      step: 'submission_unknown',
      errorCode: 'PUBLISH_CLICK_OUTCOME_UNKNOWN',
      evidence,
    };
  }
  return {
    status: 'awaiting_review',
    step: 'creator_backend_verification',
    errorCode: '',
    evidence,
  };
}

async function prepareAndMaybeSubmit(activePage, input, rule, initialMediaResult = null) {
  const prepared = await prepareForm(activePage, input, rule, initialMediaResult);
  if (input.action !== 'submit' || prepared.status !== 'awaiting_review') {
    return prepared;
  }
  const submitted = await submitPreparedPage(activePage, input, prepared.evidence || {}, rule);
  return {
    ...submitted,
    evidence: {
      ...(prepared.evidence || {}),
      ...(submitted.evidence || {}),
    },
  };
}

async function discardReviewedStaleDraft(page, rule) {
  const promptNames = rule.staleDraftPromptNames || [];
  const discardNames = rule.staleDraftDiscardNames || [];
  if (!promptNames.length || !discardNames.length) return false;
  const prompt = await uniqueVisibleExactText(page, promptNames);
  if (prompt.ambiguous || !prompt.control) return false;
  const container = prompt.control.locator(
    'xpath=ancestor::*[self::section or self::div][1]',
  );
  const discard = await firstVisibleNamedControl(container, discardNames, ['button']);
  if (!discard) return false;
  const clicked = await discard.click({ timeout: 5000 })
    .then(() => true)
    .catch(() => false);
  if (!clicked) return false;
  await prompt.control.waitFor({ state: 'hidden', timeout: 5000 }).catch(() => {});
  return !await prompt.control.isVisible().catch(() => false);
}

async function firstReviewedEditorFrame(page, prefixes, timeoutMs) {
  if (!prefixes?.length || typeof page.frames !== 'function') return null;
  const deadline = Date.now() + timeoutMs;
  do {
    const frame = page.frames().find((candidate) => (
      prefixes.some((prefix) => candidate.url().startsWith(prefix))
    ));
    if (frame) return frame;
    if (Date.now() >= deadline) break;
    await page.waitForTimeout(100);
  } while (true);
  return null;
}

function reviewedEditorWaitMs(input, rule) {
  return Math.min(input.timeoutMs, rule.editorReadyTimeoutMs || 30000);
}

async function prepareForm(activePage, input, rule, initialMediaResult = null) {
  const composerPage = activePage;
  const staleDraftDiscarded = await discardReviewedStaleDraft(activePage, rule);
  let mediaResult = initialMediaResult || (rule.uploadMediaAfterText
    ? { evidence: {}, missing: [] }
    : await uploadRequiredMedia(activePage, input, rule));
  if (staleDraftDiscarded) mediaResult.evidence.staleDraftDiscarded = true;
  if (rule.postUploadSettleMs) {
    await activePage.waitForTimeout(rule.postUploadSettleMs);
  }
  if (rule.postUploadCancelDialogNames) {
    await clickNamedInDialog(activePage, rule.postUploadCancelDialogNames);
    await activePage.waitForTimeout(200);
  }
  if (rule.postUploadDismissNames) {
    await clickNamedEventuallyExact(activePage, rule.postUploadDismissNames, 4000);
    await activePage.waitForTimeout(200);
  }
  if (rule.coverAfterPostUploadDialogs && input.content.type === 'video') {
    const coverItems = input.media.filter((item) => item.role === 'cover');
    if (coverItems.length) {
      const nativeCover = await applyNativeVideoCover(activePage, coverItems, rule.coverControl);
      mediaResult.evidence.coverUploaded = nativeCover.applied;
      mediaResult.evidence.coverAppliedAs = nativeCover.method;
      if (!nativeCover.applied) mediaResult.missing.push('media.cover');
    }
  }
  if (rule.editorFrameURLPrefixes?.length) {
    const [hostTitle, hostBody] = await Promise.all([
      firstVisible(activePage, rule.title || []),
      firstVisible(activePage, rule.body || []),
    ]);
    if (!hostTitle || !hostBody) {
      const editorFrame = await firstReviewedEditorFrame(
        activePage,
        rule.editorFrameURLPrefixes,
        reviewedEditorWaitMs(input, rule),
      );
      if (editorFrame) activePage = editorFrame;
    }
  }
  const defaultFieldWait = Math.min(30000, Math.max(3000, Math.floor(input.timeoutMs / 2)));
  const fieldWait = rule.editorReadyTimeoutMs
    ? Math.min(input.timeoutMs, rule.editorReadyTimeoutMs)
    : defaultFieldWait;
  // Title and body are independent DOM checks. Await them together so a
  // missing WeChat editor cannot consume the wait budget twice.
  const [title, body] = await Promise.all([
    firstVisibleEventually(activePage, rule.title, rule.titleOptional ? Math.min(fieldWait, 2000) : fieldWait),
    firstVisibleEventually(activePage, rule.body, rule.bodyOptional ? Math.min(fieldWait, 2000) : fieldWait),
  ]);
  const titleFilled = rule.titleOptional && !title
    ? true
    : await fillLocator(title, input.content.title);
  const bodyValue = bodyWithoutFrozenTrailingTags(input.content, rule);
  const bodyFilled = rule.bodyOptional && !body
    ? true
    : await fillLocator(body, bodyValue, {
      whitespaceEquivalent: Boolean(rule.bodyWhitespaceEquivalent),
      nativeTyping: Boolean(rule.bodyNativeTyping),
    });
  if (rule.uploadMediaAfterText) {
    mediaResult = await uploadRequiredMedia(activePage, input, rule);
  }
  const articleCoverAfterOptions = rule.articleCoverControl?.afterOptions === true;
  if (rule.articleCoverControl && !articleCoverAfterOptions
    && input.media.some((item) => ['content', 'cover'].includes(item.role))) {
    const articleCoverApplied = recordArticleCoverEvidence(
      mediaResult.evidence,
      await applyArticleCover(activePage, rule.articleCoverControl, input.media.filter((item) => item.role === 'cover')),
    );
    if (!articleCoverApplied) mediaResult.missing.push('media.cover');
  }
  if (rule.articleCoverControl && mediaResult.missing.includes('media.cover')) {
    return {
      status: 'manual_takeover',
      step: 'required_control_missing',
      errorCode: 'FORM_REVIEW_REQUIRED',
      evidence: {
        ...mediaResult.evidence,
        titleFound: Boolean(title), bodyFound: Boolean(body), titleFilled, bodyFilled,
        bodyDiagnostics: bodyFilled ? {} : await contentEditableDiagnostics(body, bodyValue),
        tagsAppliedAs: input.content.tags.length && (rule.separateTagControl || rule.topicControl) ? 'platform_topics' : '',
        requiredOptionsApplied: [],
        settingsApplied: [],
        settingsSkipped: [],
        settingsNotPresent: [],
        settingsUnsupported: [],
        unverifiedFields: [],
        missingRequiredControls: [...mediaResult.missing],
        submitFound: false,
        submitEnabled: false,
        pageInspection: await inspectPage(activePage, rule),
      },
    };
  }
  if (rule.editorReadyTimeoutMs && (!titleFilled || !bodyFilled)) {
    const missingRequiredControls = [...mediaResult.missing];
    if (!titleFilled) missingRequiredControls.push('content.title');
    if (!bodyFilled) missingRequiredControls.push('content.body');
    // Do not open WeChat's review dialog when the article itself was not
    // filled. The advance button is adjacent to an irreversible final action
    // and is not needed to explain this recoverable editor failure.
    return {
      status: 'manual_takeover',
      step: 'required_control_missing',
      errorCode: 'FORM_REVIEW_REQUIRED',
      evidence: {
        ...mediaResult.evidence,
        titleFound: Boolean(title), bodyFound: Boolean(body), titleFilled, bodyFilled,
        bodyDiagnostics: await contentEditableDiagnostics(body, bodyValue),
        tagsAppliedAs: '',
        requiredOptionsApplied: [],
        settingsApplied: [],
        settingsSkipped: [],
        settingsNotPresent: [],
        settingsUnsupported: [],
        unverifiedFields: [],
        missingRequiredControls,
        submitFound: false, submitEnabled: false,
        pageInspection: await inspectPage(activePage, rule),
      },
    };
  }
  if (input.platform === 'youtube' && mediaResult.missing.includes('media.cover')) {
    return {
      status: 'manual_takeover',
      step: 'required_control_missing',
      errorCode: 'FORM_REVIEW_REQUIRED',
      evidence: {
        ...mediaResult.evidence,
        titleFound: Boolean(title), bodyFound: Boolean(body), titleFilled, bodyFilled,
        bodyDiagnostics: bodyFilled ? {} : await contentEditableDiagnostics(body, bodyValue),
        tagsAppliedAs: '',
        requiredOptionsApplied: [],
        settingsApplied: [],
        settingsSkipped: [],
        settingsNotPresent: [],
        settingsUnsupported: [],
        unverifiedFields: [],
        missingRequiredControls: [...mediaResult.missing],
        submitFound: false, submitEnabled: false,
        pageInspection: await inspectPage(activePage, rule),
      },
    };
  }
  const topicResult = rule.topicControl && bodyFilled
    ? await commitPlatformTopics(activePage, body, input.content.tags, rule.topicControl, bodyValue)
    : {
      selected: [],
      missing: input.content.tags.length && !rule.separateTagControl && !rule.nativeTagsOptional
        ? input.content.tags
        : [],
    };
  if (input.platform === 'tiktok' || input.platform === 'youtube') {
    await revealReviewSettings(activePage, input.platform);
  }
  const settingsBeforeReview = await applySettings(activePage, input, rule);
  if (input.platform === 'youtube') {
    const detailKeys = ['audience', 'playlist', 'allow_comments', 'altered_content']
      .filter((key) => Object.hasOwn(input.settings, key));
    const provenDetails = new Set([
      ...settingsBeforeReview.applied,
      ...settingsBeforeReview.verified,
      ...settingsBeforeReview.skipped,
      ...settingsBeforeReview.notPresent,
    ]);
    const unverifiedDetails = detailKeys.filter((key) => !provenDetails.has(key));
    if (unverifiedDetails.length) {
      return {
        status: 'manual_takeover',
        step: 'form_unverified',
        errorCode: 'FORM_REVIEW_REQUIRED',
        evidence: {
          ...mediaResult.evidence,
          titleFound: Boolean(title), bodyFound: Boolean(body), titleFilled, bodyFilled,
          bodyDiagnostics: bodyFilled ? {} : await contentEditableDiagnostics(body, bodyValue),
          tagsAppliedAs: input.content.tags.length ? 'platform_topics' : '',
          selectedTopics: topicResult.selected,
          topicDiagnostics: topicResult.diagnostics || [],
          requiredOptionsApplied: [],
          settingsApplied: settingsBeforeReview.applied,
          settingsVerified: settingsBeforeReview.verified,
          settingsSkipped: settingsBeforeReview.skipped,
          settingsNotPresent: settingsBeforeReview.notPresent,
          settingsUnsupported: settingsBeforeReview.unsupported,
          unverifiedFields: unverifiedDetails.map((key) => `settings.${key}`),
          missingRequiredControls: [...mediaResult.missing],
          submitFound: false,
          submitEnabled: false,
          pageInspection: await inspectPage(activePage, rule),
        },
      };
    }
  }
  // The reviewed Douyin dialog is mounted asynchronously after the selected
  // declaration has already updated its compact field. Wait briefly so that
  // a prepared form never continues behind that modal.
  if (input.platform === 'douyin' && ['ai', 'none'].includes(input.settings.self_declaration)) await activePage.waitForTimeout(750);
  if (!await settleDouyinAIConfirmation(activePage, input)) {
    return {
      status: 'manual_takeover', step: 'required_control_missing', errorCode: 'FORM_REVIEW_REQUIRED',
      evidence: {
        ...mediaResult.evidence,
        titleFound: Boolean(title), bodyFound: Boolean(body), titleFilled, bodyFilled,
        settingsApplied: settingsBeforeReview.applied, settingsVerified: settingsBeforeReview.verified,
        settingsSkipped: settingsBeforeReview.skipped, settingsNotPresent: settingsBeforeReview.notPresent,
        settingsUnsupported: settingsBeforeReview.unsupported,
        missingRequiredControls: ['settings.self_declaration_confirmation'],
        submitFound: false, submitEnabled: false, pageInspection: await inspectPage(activePage, rule),
      },
    };
  }
  let platformOptionsResult = await prepareRequiredPlatformOptions(activePage, input, rule);
  if (articleCoverAfterOptions && input.media.some((item) => item.role === 'cover')) {
    const articleCoverResult = await applyArticleCover(
      activePage,
      rule.articleCoverControl,
      input.media.filter((item) => item.role === 'cover'),
    );
    if (articleCoverResult?.needsReopenVerification) {
      const reopened = await prepareRequiredPlatformOptions(activePage, input, rule);
      platformOptionsResult = {
        applied: [...platformOptionsResult.applied, ...reopened.applied],
        missing: [...platformOptionsResult.missing, ...reopened.missing],
      };
      const previewCommitted = await nativeArticleCoverPreviewCommitted(
        activePage,
        rule.articleCoverControl,
        rule.articleCoverControl.commitTimeoutMs || 5000,
      );
      articleCoverResult.applied = previewCommitted;
      articleCoverResult.previewCommitted = previewCommitted;
    }
    const articleCoverApplied = recordArticleCoverEvidence(mediaResult.evidence, articleCoverResult);
    if (!articleCoverApplied) mediaResult.missing.push('media.cover');
  }
  // A native CSDN cover must settle before tag or schedule popups are opened:
  // those overlays can mask the upload control and turn a failed selection
  // into an ambiguous, repeated browser interaction.
  if (rule.articleCoverControl?.beforeSeparateTags && mediaResult.missing.includes('media.cover')) {
    return {
      status: 'manual_takeover',
      step: 'required_control_missing',
      errorCode: 'FORM_REVIEW_REQUIRED',
      evidence: {
        ...mediaResult.evidence,
        titleFound: Boolean(title), bodyFound: Boolean(body), titleFilled, bodyFilled,
        bodyDiagnostics: bodyFilled ? {} : await contentEditableDiagnostics(body, bodyValue),
        tagsAppliedAs: '', requiredOptionsApplied: platformOptionsResult.applied,
        settingsApplied: [], settingsSkipped: [], settingsNotPresent: [], settingsUnsupported: [],
        unverifiedFields: [], missingRequiredControls: [...mediaResult.missing],
        submitFound: false, submitEnabled: false, pageInspection: await inspectPage(activePage, rule),
      },
    };
  }
  if (rule.separateTagControl && rule.articleCoverControl?.beforeSeparateTags) {
    const tags = await applySeparatePlatformTags(activePage, input, rule);
    platformOptionsResult.applied.push(...tags.applied);
    platformOptionsResult.missing.push(...tags.missing);
  }
  // Some official controls live in the final review panel (for example CSDN
  // article type and WeChat publish type), while author/location controls live
  // in the editor. Retry only fields not already proven in the first state.
  const handledBeforeReview = new Set([
    ...settingsBeforeReview.applied,
    ...settingsBeforeReview.verified,
    ...settingsBeforeReview.skipped,
    ...(platformOptionsResult.settingsApplied || []),
  ]);
  const remainingSettings = Object.fromEntries(
    Object.entries(input.settings).filter(([key]) => !handledBeforeReview.has(key)),
  );
  const settingsAfterReview = await applySettings(activePage, { ...input, settings: remainingSettings }, rule);
  const settingsApplied = [...new Set([
    ...settingsBeforeReview.applied,
    ...(platformOptionsResult.settingsApplied || []),
    ...settingsAfterReview.applied,
  ])];
  const settingsVerified = [...new Set([...settingsBeforeReview.verified, ...settingsAfterReview.verified])];
  const settingsSkipped = [...new Set([...settingsBeforeReview.skipped, ...settingsAfterReview.skipped])];
  const provenSettings = new Set([...settingsApplied, ...settingsVerified, ...settingsSkipped]);
  const retriedSettings = new Set(Object.keys(remainingSettings));
  const settingsNotPresent = [...new Set([
    ...settingsBeforeReview.notPresent.filter((key) => !retriedSettings.has(key)),
    ...settingsAfterReview.notPresent,
  ])]
    .filter((key) => !provenSettings.has(key));
  const settingsUnsupported = [...new Set([...settingsBeforeReview.unsupported, ...settingsAfterReview.unsupported])];
  const publishTimeResult = await applyPublishTime(activePage, input, rule);
  // Some verified-absent defaults depend on the complete composer state.
  // Recheck them after native scheduling settles; this is read-only and never
  // retries a control click.
  const handledSettings = new Set([...settingsApplied, ...settingsVerified, ...settingsSkipped, ...settingsNotPresent]);
  for (const [key, value] of Object.entries(input.settings)) {
    if (handledSettings.has(key)) continue;
    const definition = rule.settingControls?.[key];
    const absent = definition?.type === 'boolean'
      ? await verifiedAbsentBooleanSetting(activePage, rule, definition, value, input.content?.type)
      : definition?.type === 'enum'
        ? await verifiedAbsentEnumSetting(activePage, rule, definition, value, input.content?.type)
        : false;
    if (!absent) continue;
    settingsNotPresent.push(key);
    handledSettings.add(key);
  }
  const unverifiedSettings = Object.keys(input.settings)
    .filter((key) => !handledSettings.has(key))
    .map((key) => `settings.${key}`);
  const submitButton = rule.intermediateSubmitControl
    ? (await exactIntermediateSubmit(
      rule.editorFrameURLPrefixes?.length ? composerPage : activePage,
      rule.intermediateSubmitControl,
    )).control
    : rule.submitReadyTimeoutMs
      ? await (
        rule.submitRootSelector ? firstReadyExactComposerSubmit : firstReadyReviewedSubmit
      )(rule.editorFrameURLPrefixes?.length ? composerPage : activePage, rule, Math.min(input.timeoutMs, rule.submitReadyTimeoutMs))
      : await firstReviewedSubmit(rule.editorFrameURLPrefixes?.length ? composerPage : activePage, rule, 5000);
  const submitEnabled = Boolean(submitButton) && await submitButton.isEnabled().catch(() => false);
  // The CSDN cropper can show a transient image even when closing/reopening
  // publish options drops the cover. Check the actual final-review surface,
  // not the earlier cropper state, before treating a frozen cover as ready.
  if (articleCoverAfterOptions && input.media.some((item) => item.role === 'cover')) {
    const finalCoverCommitted = await nativeArticleCoverPreviewCommitted(
      activePage,
      rule.articleCoverControl,
      500,
    );
    mediaResult.evidence.articleCoverFinalPreviewCommitted = finalCoverCommitted;
    if (!finalCoverCommitted && !mediaResult.missing.includes('media.cover')) {
      mediaResult.missing.push('media.cover');
    }
  }
  const missingRequiredControls = [...mediaResult.missing];
  // Prepare-only requests must fail before they can be frozen as ready. A
  // legacy one-step submit continues to the dedicated pre-click verifier so
  // callers retain the precise MEDIA_UPLOAD_UNVERIFIED terminal result.
  if (input.action !== 'submit'
    && rule.requireUploadCompletionAtPrepare
    && !uploadCompletionVerified(input, mediaResult.evidence, rule).ok) {
    missingRequiredControls.push('media.upload_completion');
  }
  if (!titleFilled) missingRequiredControls.push('content.title');
  if (!bodyFilled) missingRequiredControls.push('content.body');
  if (topicResult.missing.length && !rule.topicControl?.optional) missingRequiredControls.push('content.tags');
  missingRequiredControls.push(...platformOptionsResult.missing);
  if (!submitEnabled) missingRequiredControls.push('submit.review_button');
  const evidence = {
    ...mediaResult.evidence,
    titleFound: Boolean(title), bodyFound: Boolean(body), titleFilled, bodyFilled,
    bodyDiagnostics: bodyFilled ? {} : await contentEditableDiagnostics(body, bodyValue),
    tagsAppliedAs: input.content.tags.length
      ? (rule.separateTagControl || rule.topicControl ? 'platform_topics' : '')
      : '',
    selectedTopics: topicResult.selected,
    topicDiagnostics: topicResult.diagnostics || [],
    requiredOptionsApplied: platformOptionsResult.applied,
    publishTimeApplied: publishTimeResult.applied.includes('publish.time'),
    publishTimeValue: publishTimeResult.value,
    publishTimeRequestedValue: publishTimeResult.requestedValue,
    publishTimeFallbackUsed: publishTimeResult.fallbackUsed,
    publishTimeMaximumValue: publishTimeResult.maximumValue,
    settingsApplied,
    settingsVerified,
    settingsSkipped,
    settingsNotPresent,
    settingsUnsupported,
    unverifiedFields: [...unverifiedSettings, ...publishTimeResult.unverified],
    missingRequiredControls,
    submitFound: Boolean(submitButton), submitEnabled,
  };
  if (input.platform === 'tiktok'
    && (unverifiedSettings.includes('settings.allow_download') || publishTimeResult.unverified.length)) {
    evidence.reviewedComposerLandmarks = await tiktokVideoComposerLandmarkState(activePage);
  }
  if (missingRequiredControls.length) {
    evidence.pageInspection = await inspectPage(activePage, rule);
    return { status: 'manual_takeover', step: 'required_control_missing', errorCode: 'FORM_REVIEW_REQUIRED', evidence };
  }
  if (unverifiedSettings.length || publishTimeResult.unverified.length) {
    evidence.pageInspection = await inspectPage(activePage, rule);
    return { status: 'manual_takeover', step: 'form_unverified', errorCode: 'FORM_REVIEW_REQUIRED', evidence };
  }
  return { status: 'awaiting_review', step: 'ready_to_submit', errorCode: '', evidence };
}

async function prepareTargetPage(context, rule, contentType = '', currentPage = null) {
  let pages = currentPage && !currentPage.isClosed()
    ? [currentPage]
    : context.pages().filter((candidate) => !candidate.isClosed());
  // Publishing stays in the account's current browser tab. A new tab is only
  // an empty-context fallback; a platform-created editor popup is handled by
  // preparePage after the reviewed entry action.
  const activeAccountPage = async () => pages[pages.length - 1] || context.newPage();
  if (rule.entryFlows?.[contentType]) return activeAccountPage();
  if (rule.alwaysFreshComposer) return activeAccountPage();
  if (rule.composerUrl) {
    const composerURL = rule.composerURLs?.[contentType] || rule.composerUrl;
    const composerPrefixes = [
      composerURL,
      ...(rule.composerURLPrefixes?.[contentType] || []),
    ];
    return newestMatchingPage(pages, composerPrefixes) || activeAccountPage();
  }
  const reviewedEntryPage = () => {
    pages = context.pages().filter((candidate) => !candidate.isClosed());
    if (rule.preferEntryPage) {
      const entryPage = newestMatchingPage(
        pages.filter((candidate) => !pageMatchesPrefixes(candidate, rule.requiredURLPrefixes)),
        rule.entryURLPrefixes,
      );
      if (entryPage) return entryPage;
    }
    // Platforms without a direct composer URL (currently WeChat MP) must keep
    // their existing creator tab so its reviewed entry control can be used.
    // Never fall back to an arbitrary last tab in the browser context.
    return newestMatchingPage(pages, rule.requiredURLPrefixes)
      || newestMatchingPage(pages, rule.entryURLPrefixes)
      || null;
  };
  let selected = reviewedEntryPage();
  if (selected) return selected;
  // A cold Chromium attach can expose only about:blank before its configured
  // creator home finishes navigating. Wait briefly for an approved URL rather
  // than treating that normal startup race as a missing publishing page.
  const deadline = Date.now() + 5000;
  do {
    await new Promise((resolve) => setTimeout(resolve, 250));
    selected = reviewedEntryPage();
    if (selected) return selected;
  } while (Date.now() < deadline);
  return null;
}

async function preparedSubmitTargetPage(context, rule, input = null) {
  const pages = context.pages().filter((candidate) => !candidate.isClosed());
  const prefixes = input
    ? requiredSubmitURLPrefixes(input, rule)
    : (rule.requiredURLPrefixes || []);
  if (!prefixes.length) return null;
  return newestMatchingPage(pages, prefixes);
}

async function recoverPreUploadTransientError(page, rule, timeoutMs = 30000) {
  const names = rule.preUploadTransientErrorNames || [];
  const visible = async () => {
    for (const name of names) {
      const matches = page.getByText(name, { exact: true });
      for (let index = 0; index < await matches.count(); index += 1) {
        if (await matches.nth(index).isVisible().catch(() => false)) return true;
      }
    }
    return false;
  };
  if (!names.length) {
    return { detected: false, reloadAttempted: false, recovered: false };
  }
  const detectionDeadline = Date.now() + (rule.preUploadTransientDetectionMs || 0);
  while (!await visible() && Date.now() < detectionDeadline) {
    await page.waitForTimeout(200);
  }
  if (!await visible()) return { detected: false, reloadAttempted: false, recovered: false };
  const retry = await uniqueVisibleNamedControl(
    page,
    rule.preUploadTransientRetryNames || [],
    ['button'],
  );
  if (!retry.ambiguous && retry.control
    && await retry.control.click({ timeout: 1500 }).then(() => true).catch(() => false)) {
    await page.waitForTimeout(1500);
    if (!await visible()) {
      return { detected: true, reloadAttempted: false, recovered: true };
    }
  }
  try {
    await page.reload({ waitUntil: 'domcontentloaded', timeout: Math.min(timeoutMs, 30000) });
    await page.waitForTimeout(1500);
  } catch (_) {
    return { detected: true, reloadAttempted: true, recovered: false };
  }
  if (!await visible()) return { detected: true, reloadAttempted: true, recovered: true };
  if (rule.preUploadTransientFallbackURL) {
    try {
      await page.goto(rule.preUploadTransientFallbackURL, {
        waitUntil: 'domcontentloaded',
        timeout: Math.min(timeoutMs, 30000),
      });
      await page.waitForTimeout(1500);
    } catch (_) {
      return {
        detected: true, reloadAttempted: true, fallbackAttempted: true, recovered: false,
      };
    }
    return {
      detected: true, reloadAttempted: true, fallbackAttempted: true, recovered: !await visible(),
    };
  }
  return { detected: true, reloadAttempted: true, recovered: false };
}

function newestMatchingPage(pages, prefixes) {
  for (let index = pages.length - 1; index >= 0; index -= 1) {
    if (pageMatchesPrefixes(pages[index], prefixes)) return pages[index];
  }
  return null;
}

function pageMatchesPrefixes(page, prefixes) {
  try {
    const location = page.url();
    return (prefixes || []).some((prefix) => location.startsWith(prefix));
  } catch (_) {
    return false;
  }
}

function reviewedEntryReachPrefixes(entryFlow) {
  return entryFlow.intermediateURLPrefixes?.length
    ? entryFlow.intermediateURLPrefixes
    : entryFlow.requiredURLPrefixes;
}

function safePageLocation(page) {
  try {
    const location = new URL(page.url());
    return `${location.origin}${location.pathname}`;
  } catch (_) {
    return '';
  }
}

async function captureFailureScreenshot(page, platform) {
  try {
    const directory = '/tmp/alpha-nexus-publish-evidence';
    fs.mkdirSync(directory, { recursive: true });
    const filePath = path.join(directory, `${platform}-${Date.now()}-${process.pid}.png`);
    await page.screenshot({ path: filePath, fullPage: false });
    return filePath;
  } catch (_) {
    return '';
  }
}

async function preparedPageMissing(page, platform, evidence = {}) {
  const screenshotPath = await captureFailureScreenshot(page, platform);
  return {
    status: 'manual_takeover',
    step: 'prepared_page_missing',
    errorCode: 'FORM_REVIEW_REQUIRED',
    evidence: {
      ...evidence,
      submitFound: false,
      entryLocation: safePageLocation(page),
      ...(screenshotPath ? { screenshotPath } : {}),
    },
  };
}

async function preparePage(page, input) {
  const rule = PLATFORM_RULES[input.platform];
  const entryFlow = rule.entryFlows?.[input.content.type];
  const activeRule = entryFlow ? { ...rule, ...entryFlow } : rule;
  const composerURL = rule.composerURLs?.[input.content.type] || rule.composerUrl;
  let activePage = page;
  let entryMediaResult = null;
  let pagesBeforeEntry;
  let entryPopup;
  let entryClicked = false;
  let contentTabClicked = false;
  let resumeDraftClicked = false;
	// Keep the long worker budget for native media processing. A stalled CDP
	// navigation must fail promptly instead of consuming the whole upload window.
	const navigationTimeoutMs = Math.min(input.timeoutMs, 30000);
  if (entryFlow) {
    await activePage.goto(entryFlow.entryURL, { waitUntil: 'domcontentloaded', timeout: navigationTimeoutMs });
    entryClicked = Boolean(await clickEntryNamedEventually(
      activePage,
      entryFlow.entryNames,
      Math.min(input.timeoutMs, 10000),
    ));
    // Some reviewed entry actions (currently YouTube Studio) open the upload
    // dialog in-place. With no intermediate namespace, the trusted entry page
    // itself is the reached surface and normal media upload continues below.
    const entryReachPrefixes = reviewedEntryReachPrefixes(entryFlow);
    let intermediateReached = false;
    if (entryClicked) {
      await activePage.waitForURL(
        (url) => entryReachPrefixes.some((prefix) => url.toString().startsWith(prefix)),
        { timeout: Math.min(input.timeoutMs, 10000) },
      ).catch(() => {});
      intermediateReached = pageMatchesPrefixes(activePage, entryReachPrefixes);
      if (intermediateReached && entryFlow.uploadOnIntermediate) {
        entryMediaResult = await uploadRequiredMedia(activePage, input, activeRule);
        if (!entryMediaResult.missing.length) {
          await activePage.waitForURL(
            (url) => entryFlow.requiredURLPrefixes.some((prefix) => url.toString().startsWith(prefix)),
            { timeout: Math.min(input.timeoutMs, rule.uploadCompletionTimeoutMs || 10000) },
          ).catch(() => {});
        }
      }
    }
    let entryFallbackUsed = false;
    if (!intermediateReached && entryFlow.fallbackEntryURL) {
      await activePage.goto(entryFlow.fallbackEntryURL, { waitUntil: 'domcontentloaded', timeout: navigationTimeoutMs });
      intermediateReached = pageMatchesPrefixes(activePage, entryReachPrefixes);
      entryFallbackUsed = intermediateReached;
      if (intermediateReached && entryFlow.uploadOnIntermediate) {
        entryMediaResult = await uploadRequiredMedia(activePage, input, activeRule);
        if (!entryMediaResult.missing.length) {
          await activePage.waitForURL(
            (url) => entryFlow.requiredURLPrefixes.some((prefix) => url.toString().startsWith(prefix)),
            { timeout: Math.min(input.timeoutMs, rule.uploadCompletionTimeoutMs || 10000) },
          ).catch(() => {});
        }
      }
    }
    if ((!entryClicked && !entryFallbackUsed) || !intermediateReached || !pageMatchesPrefixes(activePage, entryFlow.requiredURLPrefixes)) {
      return preparedPageMissing(activePage, input.platform, {
          ...(entryMediaResult?.evidence || {}),
          entryClicked,
		  entryFallbackUsed,
          intermediateReached,
          intermediateUploadAttempted: Boolean(entryMediaResult),
          missingRequiredControls: entryMediaResult?.missing || [],
      });
    }
  } else if (composerURL && (
    activeRule.alwaysFreshComposer
    || !pageMatchesPrefixes(activePage, [composerURL])
  )) {
    try {
      await activePage.goto(composerURL, { waitUntil: 'domcontentloaded', timeout: navigationTimeoutMs });
    } catch (_) {
      return { status: 'permanent_failed', step: 'composer_navigation', errorCode: 'COMPOSER_NAVIGATION_FAILED', evidence: {} };
    }
  } else if (!rule.composerUrl && rule.entryNames && !pageMatchesPrefixes(activePage, rule.requiredURLPrefixes)) {
    if (!pageMatchesPrefixes(activePage, rule.entryURLPrefixes)) {
      return preparedPageMissing(activePage, input.platform);
    }
    pagesBeforeEntry = new Set(activePage.context().pages().filter((candidate) => !candidate.isClosed()));
    // Arm this before clicking. Some WeChat creator views hand the editor to
    // a popup, while others navigate the current tab; both are accepted only
    // after the exact appmsg URL is observed below.
    entryPopup = activePage.context().waitForEvent('page', { timeout: 2500 }).catch(() => null);
    const entry = await firstVisible(activePage, rule.entrySelectors || []);
    if (entry) {
      await entry.click();
      entryClicked = true;
    } else {
      entryClicked = Boolean(await clickNamed(activePage, rule.entryNames));
    }
  }
  await activePage.waitForTimeout(1000);
  const transientRecovery = await recoverPreUploadTransientError(
    activePage,
    activeRule,
    navigationTimeoutMs,
  );
  if (transientRecovery.detected && !transientRecovery.recovered) {
    const screenshotPath = await captureFailureScreenshot(activePage, input.platform);
    return {
      status: 'manual_takeover',
      step: 'composer_transient_error',
      errorCode: 'FORM_REVIEW_REQUIRED',
      evidence: {
        transientComposerRecovery: transientRecovery,
        submitFound: false,
        submitEnabled: false,
        ...(screenshotPath ? { screenshotPath } : {}),
      },
      location: safePageLocation(activePage),
    };
  }
  if (pagesBeforeEntry && entryClicked && !pageMatchesPrefixes(activePage, rule.requiredURLPrefixes)) {
    // A reviewed entry action may open an editor popup. Adopt only a page
    // created by this action whose URL is already in the approved editor
    // prefix; ignore every other popup or user tab.
    let editorPopup = await entryPopup;
    if (editorPopup && !editorPopup.isClosed()) {
      await editorPopup.waitForURL((url) => (rule.requiredURLPrefixes || []).some((prefix) => url.toString().startsWith(prefix)), { timeout: 3000 }).catch(() => {});
      if (!pageMatchesPrefixes(editorPopup, rule.requiredURLPrefixes)) editorPopup = null;
    }
    // A popup event can be missed if it was created before its listener was
    // attached by the browser engine. The bounded page scan keeps that race
    // recoverable without ever adopting a pre-existing or unrelated tab.
    if (!editorPopup) {
      const deadline = Date.now() + 5000;
      do {
        editorPopup = newestMatchingPage(
          activePage.context().pages().filter((candidate) => (
            !candidate.isClosed() && !pagesBeforeEntry.has(candidate)
          )),
          rule.requiredURLPrefixes,
        );
        if (editorPopup || Date.now() >= deadline) break;
        await activePage.waitForTimeout(250);
      } while (true);
    }
    if (editorPopup) activePage = editorPopup;
  }
  // Keep the page selected by prepareTargetPage. A platform navigation may
  // race with a user opening another tab; only the scoped editor popup above
  // can replace it.
  if (activeRule.requiredURLPrefixes?.length && !pageMatchesPrefixes(activePage, activeRule.requiredURLPrefixes)) {
    return preparedPageMissing(activePage, input.platform);
  }
  if (activeRule.dismissNames) {
    await clickNamed(activePage, activeRule.dismissNames);
    await activePage.waitForTimeout(200);
  }
  const contentTabNames = activeRule.contentTabNames?.[input.content.type] || [];
  if (contentTabNames.length) {
    const uploadTriggerNames = activeRule.mediaUploadTriggerNames?.image || [];
    let imageInput = await uniqueFileInput(activePage, 'image');
    let uploadTrigger = await uniqueVisibleExactText(activePage, uploadTriggerNames);
    let resumeDraftButton = await firstVisibleNamedControl(
      activePage,
      activeRule.resumeDraftNames || [],
      ['button'],
    );
    let resumeDraft = await uniqueVisibleExactText(activePage, activeRule.resumeDraftNames || []);
    let contentModeReady = Boolean(imageInput.locator)
      || (!uploadTrigger.ambiguous && Boolean(uploadTrigger.control))
      || Boolean(resumeDraftButton)
      || (!resumeDraft.ambiguous && Boolean(resumeDraft.control));
    if (!contentModeReady) {
      contentTabClicked = Boolean(await clickNamedEventuallyExact(
        activePage,
        contentTabNames,
        Math.min(input.timeoutMs, 10000),
      ));
      if (contentTabClicked) {
        imageInput = await uniqueFileInput(activePage, 'image');
        uploadTrigger = await uniqueVisibleExactText(activePage, uploadTriggerNames);
        resumeDraftButton = await firstVisibleNamedControl(
          activePage,
          activeRule.resumeDraftNames || [],
          ['button'],
        );
        resumeDraft = await uniqueVisibleExactText(activePage, activeRule.resumeDraftNames || []);
        contentModeReady = Boolean(imageInput.locator)
          || (!uploadTrigger.ambiguous && Boolean(uploadTrigger.control))
          || Boolean(resumeDraftButton)
          || (!resumeDraft.ambiguous && Boolean(resumeDraft.control));
      }
    }
    if (!contentModeReady) {
      return preparedPageMissing(activePage, input.platform, { contentTabClicked: false });
    }
    // Kuaishou mounts the editor only after native image selection. The upload
    // proof below remains the first permitted file action and verifies the
    // selected files, preview, processing state, body, and final control.
    const resumeDraftControl = resumeDraftButton
      || (!resumeDraft.ambiguous ? resumeDraft.control : null);
    if (resumeDraftControl) {
      resumeDraftClicked = await resumeDraftControl.click({ timeout: 5000 })
        .then(() => true)
        .catch(() => false);
      if (!resumeDraftClicked) {
        return preparedPageMissing(activePage, input.platform, {
          contentTabClicked,
          resumeDraftClicked: false,
        });
      }
    }
  }
  let titleActivated = false;
  const titleActivator = await firstVisible(activePage, activeRule.titleActivationSelectors || []);
  if (titleActivator) {
    try {
      await titleActivator.click({ timeout: 1500 });
      titleActivated = true;
    } catch (_) {
      // Fall back to the reviewed fresh-draft title text below.
    }
  }
  if (!titleActivated && activeRule.titleActivationNames) {
    titleActivated = Boolean(await clickNamed(activePage, activeRule.titleActivationNames));
  }
  if (titleActivated) {
    await activePage.waitForTimeout(300);
  }
  const result = await prepareAndMaybeSubmit(activePage, input, activeRule, entryMediaResult);
  if (transientRecovery.detected) {
    result.evidence = {
      ...(result.evidence || {}),
      transientComposerRecovery: transientRecovery,
    };
  }
  if (contentTabNames.length) {
    result.evidence = {
      ...(result.evidence || {}),
      contentTabSelected: true,
      contentTabClicked,
      resumeDraftClicked,
    };
  }
  if (result.status === 'manual_takeover') {
    const screenshotPath = await captureFailureScreenshot(activePage, input.platform);
    if (screenshotPath) result.evidence = { ...(result.evidence || {}), screenshotPath };
  }
  return { ...result, location: safePageLocation(activePage) };
}

// resumeTopics is deliberately narrower than prepare: it works only in the
// already-open reviewed editor, never navigates and never reaches a file
// input. It lets a stopped draft repair native platform topics without a
// duplicate upload.
async function resumeTopics(page, input) {
  const rule = PLATFORM_RULES[input.platform];
  const activeRule = rule.entryFlows?.[input.content.type] ? { ...rule, ...rule.entryFlows[input.content.type] } : rule;
  if (activeRule.requiredURLPrefixes?.length && !pageMatchesPrefixes(page, activeRule.requiredURLPrefixes)) {
    return preparedPageMissing(page, input.platform);
  }
  const [title, body] = await Promise.all([
    firstVisibleEventually(page, activeRule.title, Math.min(input.timeoutMs, 15000)),
    firstVisibleEventually(page, activeRule.body, Math.min(input.timeoutMs, 15000)),
  ]);
  const titleFilled = activeRule.titleOptional && !title ? true : await fillLocator(title, input.content.title);
  const bodyValue = bodyWithoutFrozenTrailingTags(input.content, activeRule);
  const bodyFilled = activeRule.bodyOptional && !body ? true : await fillLocator(body, bodyValue, {
    whitespaceEquivalent: Boolean(activeRule.bodyWhitespaceEquivalent),
    nativeTyping: Boolean(activeRule.bodyNativeTyping),
  });
  const topics = bodyFilled && activeRule.topicControl
    ? await commitPlatformTopics(page, body, input.content.tags, activeRule.topicControl, bodyValue)
    : {
      selected: [],
      missing: activeRule.nativeTagsOptional ? [] : input.content.tags,
      diagnostics: [],
    };
  const missing = [];
  if (!titleFilled) missing.push('content.title');
  if (!bodyFilled) missing.push('content.body');
  if (topics.missing.length && !activeRule.topicControl?.optional) missing.push('content.tags');
  return {
    status: topics.missing.length || !titleFilled || !bodyFilled ? 'manual_takeover' : 'resumed',
    step: topics.missing.length || !titleFilled || !bodyFilled ? 'required_control_missing' : 'topics_resumed',
    errorCode: topics.missing.length || !titleFilled || !bodyFilled ? 'FORM_REVIEW_REQUIRED' : '',
    evidence: {
      titleFound: Boolean(title), bodyFound: Boolean(body), titleFilled, bodyFilled,
      selectedTopics: topics.selected, topicDiagnostics: topics.diagnostics || [],
      missingRequiredControls: missing,
      pageInspection: await inspectPage(page, activeRule),
    },
    location: safePageLocation(page),
  };
}

// Kuaishou has no stable direct manager URL in the reviewed contract. Enter
// its manager only through the exact sidebar item already rendered for the
// logged-in creator, then return booleans rather than page text or URLs.
async function inspectKuaishouManager(page, input) {
  if (!['kuaishou', 'csdn', 'wechat_video', 'tiktok', 'youtube'].includes(input.platform)) {
    return { status: 'manual_takeover', step: 'manager_inspection_unsupported', errorCode: 'FORM_REVIEW_REQUIRED', location: safePageLocation(page), evidence: {} };
  }
  const before = safePageLocation(page);
  const alreadyManager = (input.platform === 'tiktok'
    && before === 'https://www.tiktok.com/tiktokstudio/content')
    || (input.platform === 'youtube'
      && /^https:\/\/studio\.youtube\.com\/channel\/[^/]+\/videos(?:\/|$)/u.test(before));
  const entry = alreadyManager || await clickEntryNamedEventually(
    page,
    input.platform === 'csdn'
      ? ['文章管理', '内容管理']
      : input.platform === 'youtube'
        ? ['Content', '内容']
      : input.platform === 'tiktok'
        ? ['Manage posts', '管理帖子', '作品']
        : ['内容管理'],
    Math.min(10000, input.timeoutMs),
  );
  if (!entry) {
    const screenshotPath = await captureFailureScreenshot(page, input.platform);
    return {
      status: 'manual_takeover',
      step: 'manager_entry_missing',
      errorCode: 'FORM_REVIEW_REQUIRED',
      location: safePageLocation(page),
      evidence: screenshotPath ? { screenshotPath } : {},
    };
  }
  if (!alreadyManager) await page.waitForTimeout(Math.min(3000, input.timeoutMs));
  let managerScope = page;
  if (input.platform === 'wechat_video') {
    await clickEntryNamedEventually(page, ['视频'], Math.min(5000, input.timeoutMs));
    await page.waitForTimeout(Math.min(2000, input.timeoutMs));
    let managerFrame = await firstReviewedEditorFrame(
      page,
      ['https://channels.weixin.qq.com/micro/content/post/list'],
      1000,
    );
    // The Channels shell can update only its outer URL and leave the prior
    // composer iframe mounted. Reload that exact reviewed manager URL once;
    // this is read-only navigation and never returns to the composer.
    if (!managerFrame && safePageLocation(page) === 'https://channels.weixin.qq.com/platform/post/list') {
      await page.reload({ waitUntil: 'domcontentloaded', timeout: Math.min(30000, input.timeoutMs) }).catch(() => {});
      managerFrame = await firstReviewedEditorFrame(
        page,
        ['https://channels.weixin.qq.com/micro/content/post/list'],
        Math.min(5000, input.timeoutMs),
      );
    }
    if (managerFrame) managerScope = managerFrame;
  }
  if (input.platform === 'youtube') {
    const expectedTitle = String(input.content?.title || '').trim();
    const expectedTitles = youtubeManagerTitleCandidates(expectedTitle);
    let rowEvidence = {
      matchingTitle: false, scheduledState: false, publishedState: false,
      observedStatus: '', postURL: '', externalPostID: '',
    };
    const titleDeadline = Date.now() + Math.min(3000, input.timeoutMs);
    do {
      rowEvidence = await youtubeManagerRowEvidence(page, expectedTitles);
      const expectedRowFound = input.publishAt
        ? rowEvidence.scheduledState || rowEvidence.publishedState
        : rowEvidence.matchingTitle;
      if (expectedRowFound || Date.now() >= titleDeadline) break;
      await page.waitForTimeout(100);
    } while (true);
    const expectedRowFound = input.publishAt
      ? rowEvidence.scheduledState || rowEvidence.publishedState
      : rowEvidence.matchingTitle;
    if (!expectedRowFound
      && await clickEntryNamedEventually(page, ['Shorts', '短视频'], Math.min(3000, input.timeoutMs))) {
      await page.waitForTimeout(Math.min(3000, input.timeoutMs));
    }
  }
  const visible = async (names, selectors = []) => {
    const match = await uniqueVisibleExactText(managerScope, names, selectors);
    return !match.ambiguous && Boolean(match.control);
  };
  const anyVisibleContains = async (name) => {
    const expected = normalizedSettingText(name).replace(/[\s|]/gu, '');
    if (!expected) return false;
    const matches = managerScope.locator('body *');
    for (let index = 0; index < Math.min(await matches.count(), 500); index += 1) {
      const candidate = matches.nth(index);
      if (!await candidate.isVisible().catch(() => false)) continue;
      const actual = normalizedSettingText(await candidate.textContent().catch(() => '')).replace(/[\s|]/gu, '');
      if (actual.includes(expected)) return true;
    }
    return false;
  };
  let scheduledTime = input.platform !== 'wechat_video' && input.platform !== 'tiktok'
    || !input.publishAt;
  if (!scheduledTime && input.platform === 'wechat_video') {
    scheduledTime = await visible([localPublishTimeParts(input.publishAt).dateTimeMinute]);
  }
  if (!scheduledTime && input.platform === 'tiktok') {
    const parts = schedulePartsAtMinuteStep(
      localPublishTimeParts(input.publishAt),
      PLATFORM_RULES.tiktok.scheduleControl.tiktokDateTimePicker.minuteStep,
    );
    const [year, month, day] = parts.date.split('-').map(Number);
    const localizedDateTimes = [
      `${month}月${day}日 ${parts.time}`,
      `${year}年${month}月${day}日 ${parts.time}`,
    ];
    for (const label of localizedDateTimes) {
      if (await anyVisibleContains(label)) {
        scheduledTime = true;
        break;
      }
    }
    scheduledTime = scheduledTime
      || (await visible(youtubeScheduleDateLabels(parts), ['time', 'span', 'div'])
      && await visible([
        youtubeScheduleTimeLabel(parts),
        youtubeScheduleTimeLabel(parts).replace('\u202f', ' '),
      ], ['time', 'span', 'div']));
  }
  const managerContentMarker = input.platform === 'tiktok'
    ? String(input.content?.caption || input.content?.body || '').split(/\r?\n/).map((line) => line.trim()).find(Boolean) || ''
    : String(input.content?.title || '');
  const youtubeRowEvidence = input.platform === 'youtube'
    ? await youtubeManagerRowEvidence(managerScope, youtubeManagerTitleCandidates(managerContentMarker))
    : null;
  const matchingTitle = Boolean(managerContentMarker) && (
    input.platform === 'tiktok'
      ? await anyVisibleContains(managerContentMarker)
      : input.platform === 'youtube'
        ? youtubeRowEvidence.matchingTitle
        : await visible([managerContentMarker])
  );
  const explicitScheduledState = input.platform === 'youtube'
    ? youtubeRowEvidence.scheduledState
    : await visible(['定时发布', '定时发布中', '定时发表', '待发布', 'Scheduled']);
  const evidence = {
    managerNavigation: true,
    managerLocationChanged: safePageLocation(page) !== before,
    matchingTitle,
    // TikTok Studio represents a scheduled card with a clock and timestamp;
    // it does not render a separate "Scheduled" label in every locale.
    scheduledState: explicitScheduledState || (input.platform === 'tiktok' && matchingTitle && scheduledTime),
    scheduledTime,
    publishedState: input.platform === 'youtube'
      ? youtubeRowEvidence.publishedState
      : await visible(input.platform === 'csdn' ? ['已发布', '发布成功', '发表成功'] : ['已发布', '发布成功']),
    observedStatus: input.platform === 'youtube' ? youtubeRowEvidence.observedStatus : '',
    postURL: input.platform === 'youtube' ? youtubeRowEvidence.postURL : '',
    externalPostID: input.platform === 'youtube' ? youtubeRowEvidence.externalPostID : '',
    emptyState: await visible(['暂无作品', '暂未发布作品']),
  };
  if (evidence.scheduledState && input.publishAt) {
    evidence.effectiveScheduledAt = effectiveNativePublishAt(input.platform, input.publishAt);
  }
  const hasResultEvidence = evidence.matchingTitle || evidence.scheduledState || evidence.publishedState || evidence.emptyState;
  // "定时发布" also exists on the composer. It is manager evidence only after
  // navigation changed; otherwise require a result that cannot come from the
  // composer itself.
  const hasManagerSurfaceEvidence = alreadyManager || evidence.managerLocationChanged || evidence.matchingTitle || evidence.publishedState || evidence.emptyState;
  if (!hasResultEvidence || !hasManagerSurfaceEvidence
    || (['wechat_video', 'tiktok'].includes(input.platform)
      && (!evidence.matchingTitle || !evidence.scheduledState || !evidence.scheduledTime))) {
    const screenshotPath = await captureFailureScreenshot(page, input.platform);
    if (screenshotPath) evidence.screenshotPath = screenshotPath;
    return { status: 'manual_takeover', step: 'manager_navigation_unverified', errorCode: 'FORM_REVIEW_REQUIRED', location: safePageLocation(page), evidence };
  }
  const screenshotPath = await captureFailureScreenshot(page, input.platform);
  if (screenshotPath) evidence.screenshotPath = screenshotPath;
  return { status: 'inspected', step: 'inspect_manager', errorCode: '', location: safePageLocation(page), evidence };
}

function managerInspectionPage(pages, activePage, platform) {
  if (platform !== 'wechat_video') return activePage;
  // The login helper may leave a newer /login.html tab after the actual
  // creator console has authenticated. Manager inspection must reuse the
  // existing platform tab instead of treating that stale helper as the
  // account surface.
  return newestMatchingPage(
    (pages || []).filter((candidate) => (
      !candidate.isClosed()
      && !safePageLocation(candidate).startsWith('https://channels.weixin.qq.com/login')
    )),
    ['https://channels.weixin.qq.com/platform'],
  ) || activePage;
}

async function resolveLoopbackCDPWebSocket(cdpURL, fetcher = globalThis.fetch) {
  const endpoint = new URL(cdpURL);
  endpoint.pathname = '/json/version';
  endpoint.search = '';
  endpoint.hash = '';
  let response;
  try {
    response = await fetcher(endpoint.toString(), { signal: AbortSignal.timeout(15000) });
  } catch (_) {
    throw new Error('CDP_CONNECT_FAILED');
  }
  if (!response?.ok) throw new Error('CDP_CONNECT_FAILED');
  let payload;
  try {
    payload = await response.json();
  } catch (_) {
    throw new Error('CDP_CONNECT_FAILED');
  }
  let socket;
  try {
    socket = new URL(payload?.webSocketDebuggerUrl || '');
  } catch (_) {
    throw new Error('CDP_CONNECT_FAILED');
  }
  if (socket.protocol !== 'ws:' || socket.hostname !== '127.0.0.1' || socket.port !== endpoint.port || !socket.pathname.startsWith('/devtools/browser/')) {
    throw new Error('CDP_CONNECT_FAILED');
  }
  return socket.toString();
}

function localCDPConnectionOptions(timeoutMs) {
  return {
    timeout: Math.min(timeoutMs, 15000),
    // This Worker shares the local filesystem with BrowserFacade. Disabling
    // Playwright's default context overrides avoids changing a user's active
    // fingerprint browser merely to attach a reviewed publish task.
    isLocal: true,
    noDefaults: true,
  };
}

const AGENT_PLATFORM_NAMES = Object.freeze({
  xhs: '小红书',
  douyin: '抖音',
  kuaishou: '快手',
  wechat_video: '视频号',
  wechat_mp: '微信公众号',
  csdn: 'CSDN',
  youtube: 'YouTube',
  tiktok: 'TikTok',
});

function workerAgentTaskLabel(input) {
  const platform = AGENT_PLATFORM_NAMES[input?.platform] || '平台';
  const action = {
    prepare: '内容发布准备',
    submit: '一键发布',
    submit_prepared: '一键发布',
    resume_topics: '话题补全',
    delete_content: '内容删除',
  }[input?.action] || '浏览器自动化任务';
  return `${platform} ${action}`;
}

async function withAgentControl(page, taskLabel, operation, factory = createAgentControl) {
  const control = await factory(page, { taskLabel });
  try {
    return await control.race(operation);
  } finally {
    await control.dispose();
  }
}

async function execute(input) {
  validateInput(input);
  const { chromium } = require('playwright');
  let browser;
  try {
    const wsEndpoint = await resolveLoopbackCDPWebSocket(input.cdpUrl);
    browser = await chromium.connectOverCDP(wsEndpoint, localCDPConnectionOptions(input.timeoutMs));
  } catch (_) {
    throw new Error('CDP_CONNECT_FAILED');
  }
  let context;
  let pages;
  try {
    context = browser.contexts()[0];
    pages = context ? context.pages() : [];
  } catch (_) {
    throw new Error('CDP_PAGE_LIST_FAILED');
  }
  const activePage = pages[pages.length - 1];
  if (!activePage) throw new Error('ACTIVE_PAGE_NOT_FOUND');
  if (input.action === 'inspect' || input.action === 'inspect_settings') {
    const revealedSection = input.action === 'inspect_settings'
      ? await revealReviewSettings(activePage, input.platform)
      : '';
    const evidence = await inspectPage(activePage, PLATFORM_RULES[input.platform]);
    if (input.platform === 'douyin' && input.content.type === 'video') {
      evidence.reviewedComposerLandmarks = await douyinVideoComposerLandmarkState(activePage);
      evidence.committedCoverPreview = await nativeVideoCoverPreviewCommitted(
        activePage,
        PLATFORM_RULES.douyin.coverControl,
      );
    }
    if (revealedSection) evidence.revealedSection = revealedSection;
    return { status: 'inspected', step: input.action, errorCode: '', location: safePageLocation(activePage), evidence };
  }
  if (input.action === 'inspect_composer') {
    const rule = PLATFORM_RULES[input.platform];
    const composerURL = rule.composerURLs?.[input.content.type] || rule.composerUrl;
    if (!composerURL || rule.entryFlows?.[input.content.type]) {
      return { status: 'manual_takeover', step: 'composer_inspection_unsupported', errorCode: 'FORM_REVIEW_REQUIRED', location: safePageLocation(activePage), evidence: {} };
    }
    const page = await prepareTargetPage(context, rule, input.content.type, activePage);
    if (!page) return preparedPageMissing(activePage, input.platform);
    if (!page.url().startsWith(composerURL)) {
      await page.goto(composerURL, { waitUntil: 'domcontentloaded', timeout: Math.min(input.timeoutMs, 30000) });
    }
    // Creator consoles often hydrate their editor after DOMContentLoaded.
    // This is read-only waiting, not a form interaction; sampling immediately
    // would misclassify a still-loading page as a selector regression.
    await page.waitForTimeout(Math.min(5000, input.timeoutMs));
    return { status: 'inspected', step: 'inspect_composer', errorCode: '', location: safePageLocation(page), evidence: await inspectPage(page, rule) };
  }
  if (input.action === 'inspect_manager') {
    return inspectKuaishouManager(managerInspectionPage(pages, activePage, input.platform), input);
  }
  if (input.action === 'delete_content') {
    if (input.platform !== 'youtube') {
      return { status: 'manual_takeover', step: 'delete_unsupported', errorCode: 'FORM_REVIEW_REQUIRED', location: safePageLocation(activePage), evidence: {} };
    }
    const result = await withAgentControl(
      activePage,
      workerAgentTaskLabel(input),
      () => deleteYouTubeManagerContent(activePage, input),
    );
    return { ...result, location: safePageLocation(activePage) };
  }
  if (input.action === 'resume_topics') {
    return withAgentControl(
      activePage,
      workerAgentTaskLabel(input),
      () => resumeTopics(activePage, input),
    );
  }
  let page;
  try {
    page = input.action === 'submit_prepared'
      ? await preparedSubmitTargetPage(context, PLATFORM_RULES[input.platform], input)
      : await prepareTargetPage(context, PLATFORM_RULES[input.platform], input.content.type, activePage);
  } catch (_) {
    return { status: 'permanent_failed', step: 'target_page', errorCode: 'WORKER_TARGET_PAGE_FAILED', evidence: {} };
  }
  if (!page) return preparedPageMissing(activePage, input.platform);
  if (input.action === 'submit_prepared') {
    const result = await withAgentControl(
      page,
      workerAgentTaskLabel(input),
      () => submitPreparedPage(page, input, {}, PLATFORM_RULES[input.platform]),
    );
    if (result.status === 'manual_takeover' && result.step !== 'user_takeover') {
      const screenshotPath = await captureFailureScreenshot(page, input.platform);
      if (screenshotPath) result.evidence = { ...(result.evidence || {}), screenshotPath };
    }
    return { ...result, location: safePageLocation(page) };
  }
  try {
    return await withAgentControl(
      page,
      workerAgentTaskLabel(input),
      () => preparePage(page, input),
    );
  } catch (error) {
    const screenshotPath = await captureFailureScreenshot(page, input.platform);
    return {
      status: 'permanent_failed', step: 'form_prepare',
      errorCode: classifyWorkerFailure(error),
      evidence: screenshotPath ? { screenshotPath } : {},
    };
  }
}

// Keep process-boundary diagnostics useful without returning a browser URL,
// selector, page text, or Node stack trace to the Go control plane.
function classifyWorkerFailure(error) {
  const message = String(error?.message || error || '');
  const reviewed = new Set([
    'ACTION_UNSUPPORTED', 'ACTIVE_PAGE_NOT_FOUND', 'AUTO_SUBMIT_NOT_SUPPORTED',
    'CDP_CONNECT_FAILED', 'CDP_PAGE_LIST_FAILED', 'CDP_URL_INVALID',
    'CONTENT_INVALID', 'CONTENT_TYPE_INVALID', 'CONTENT_TYPE_UNSUPPORTED',
    'CONTENT_UNKNOWN_FIELD', 'INPUT_TOO_LARGE', 'INPUT_UNKNOWN_FIELD',
    'DELETE_APPROVAL_REQUIRED', 'DELETE_FLAG_INVALID',
    'MEDIA_INVALID', 'MEDIA_ITEM_INVALID', 'MEDIA_ITEM_UNKNOWN_FIELD',
    'PLATFORM_UNSUPPORTED', 'PUBLISH_TIME_INVALID', 'SCHEMA_VERSION_UNSUPPORTED',
    'SETTINGS_INVALID', 'SETTINGS_UNKNOWN_FIELD', 'SETTINGS_UNSUPPORTED',
    'SUBMIT_APPROVAL_REQUIRED', 'SUBMIT_FLAG_INVALID', 'TAGS_INVALID', 'TIMEOUT_INVALID',
  ]);
  if (reviewed.has(message)) return message;
  if (/target page, context or browser has been closed/i.test(message)) return 'CDP_TARGET_CLOSED';
  if (/cannot find module ['\"]playwright['\"]/i.test(message)) return 'PLAYWRIGHT_RUNTIME_UNAVAILABLE';
  if (/timeout/i.test(message)) return 'WORKER_ACTION_TIMEOUT';
  if (/typeerror|is not a function|cannot read properties/i.test(message)) return 'WORKER_RUNTIME_TYPE_ERROR';
  return 'WORKER_EXECUTION_FAILED';
}

async function main() {
  try {
    const chunks = [];
    let size = 0;
    for await (const chunk of process.stdin) {
      size += chunk.length;
      if (size > 1024 * 1024) throw new Error('INPUT_TOO_LARGE');
      chunks.push(chunk);
    }
    const raw = Buffer.concat(chunks).toString('utf8').trim();
    const result = await execute(JSON.parse(raw));
    await new Promise((resolve) => process.stdout.write(`${JSON.stringify({ schemaVersion: 1, ok: true, ...result })}\n`, resolve));
    // connectOverCDP has no public disconnect method. Exiting this isolated
    // worker drops only its WebSocket; it does not send Browser.close.
    process.exit(0);
  } catch (error) {
    await new Promise((resolve) => process.stdout.write(`${JSON.stringify({ schemaVersion: 1, ok: false, status: 'permanent_failed', step: 'worker', errorCode: classifyWorkerFailure(error) })}\n`, resolve));
    process.exit(1);
  }
}

if (require.main === module) main();
module.exports = { validateInput, PLATFORM_RULES, submitSelectorCandidates, prepareAndMaybeSubmit, submitPreparedPage, clickReviewedSubmitOnce, exactComposerSubmit, xhsComposerModeMatches, prepareForm, preparePage, prepareTargetPage, preparedSubmitTargetPage, recoverPreUploadTransientError, newestMatchingPage, reviewedEntryReachPrefixes, discardReviewedStaleDraft, contentEditableValueMatches, hasTrailingTopicMarker, applyPublishTime, applySettings, applyNativeVideoCover, nativeVideoCoverPreviewCommitted, settleDouyinAIConfirmation, reviewedEditorWaitMs, immutableFilePayloadBundle, reviewedUploadSnapshot, inspectPage, revealReviewSettings, resumeTopics, inspectKuaishouManager, deleteYouTubeManagerContent, managerInspectionPage, classifyWorkerFailure, resolveLoopbackCDPWebSocket, localCDPConnectionOptions, withAgentControl, workerAgentTaskLabel };
