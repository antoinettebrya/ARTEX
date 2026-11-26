package contentengine

import (
	"fmt"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type briefLocale struct {
	title        string
	description  string
	headings     [3]string
	questions    [2]string
	intentAngles [4]string
	needs        [5]string
	image        string
	factRules    [2]string
}

var briefLocales = map[string]briefLocale{
	"en": {
		title: "%s: a practical guide", description: "Learn what to check for %s, how to act on the evidence and when to verify the result.",
		headings:     [3]string{"%s: what to check", "%s: practical steps", "Check the result"},
		questions:    [2]string{"What should you check for %s?", "How can readers verify the result?"},
		intentAngles: [4]string{"How to compare options for %s", "What to prepare before proceeding with %s", "How to find the official entry point for %s", "What to verify locally for %s"},
		needs:        [5]string{"Understand the topic and apply the guidance.", "Compare options, costs and trade-offs.", "Check requirements before completing an action.", "Find the correct official entry point.", "Check local availability, eligibility and conditions."},
		image:        "An annotated view of the key detail for %s",
		factRules:    [2]string{"Cite verified first-party evidence for quantities, dates, named specifications and prices.", "Check market, effective date and source conflicts before stating any requirement."},
	},
	"zh": {
		title: "%s实用指南", description: "围绕%s说明需要核对的条件、可执行步骤和结果验证方法。",
		headings:     [3]string{"%s要核对哪些条件", "%s的实际操作步骤", "验证结果并处理例外"},
		questions:    [2]string{"处理%s前需要核对什么？", "怎样验证结果是否符合预期？"},
		intentAngles: [4]string{"怎样比较%s的不同选项", "办理%s前要准备什么", "如何找到%s的官方入口", "在当地处理%s需要核对什么"},
		needs:        [5]string{"理解主题和具体做法。", "比较不同选项的成本和适用条件。", "核对约束并完成所需操作。", "找到可靠的官方入口。", "确认当地服务、资格及限制。"},
		image:        "标注%s的关键部位或步骤",
		factRules:    [2]string{"涉及数字、日期、规格和价格时，引用可核实的一手资料。", "明确市场和生效日期，核对来源是否冲突。"},
	},
	"ja": {
		title: "%sの実践ガイド", description: "%sの確認ポイントと具体的な手順、結果の確かめ方を紹介します。",
		headings:     [3]string{"%sで確認すること", "%sの具体的な手順", "結果の確かめ方"},
		questions:    [2]string{"%sを始める前に何を確認しますか？", "結果が正しいかどうか、どう確かめますか？"},
		intentAngles: [4]string{"%sの選択肢を比較する", "%sの手続きに必要な準備", "%sの公式窓口を見つける", "%sの地域ごとの条件を確認する"},
		needs:        [5]string{"仕組みや具体的な方法を知りたい。", "選択肢の費用や条件を比較したい。", "手続きに必要な条件を確認したい。", "公式の窓口を見つけたい。", "地域ごとの利用条件を知りたい。"},
		image:        "%sの重要箇所を示す注釈付きの図",
		factRules:    [2]string{"数字、日付、仕様、価格は確認できる一次資料を示す。", "対象地域と適用日を確認し、資料間の食い違いを記録する。"},
	},
	"th": {
		title: "คู่มือใช้งานจริงเรื่อง%s", description: "ดูสิ่งที่ต้องเตรียม ขั้นตอนที่ทำได้จริง และวิธีตรวจผลสำหรับ%s",
		headings:     [3]string{"สิ่งที่ต้องตรวจสอบเกี่ยวกับ%s", "ขั้นตอนปฏิบัติสำหรับ%s", "วิธีตรวจสอบผล"},
		questions:    [2]string{"ก่อนเริ่ม%sควรตรวจสอบอะไรบ้าง", "จะรู้ได้อย่างไรว่าผลตรงตามที่ต้องการ"},
		intentAngles: [4]string{"วิธีเปรียบเทียบทางเลือกสำหรับ%s", "สิ่งที่ต้องเตรียมก่อนดำเนินการเรื่อง%s", "วิธีค้นหาช่องทางทางการสำหรับ%s", "เงื่อนไขในพื้นที่ที่ควรตรวจสอบสำหรับ%s"},
		needs:        [5]string{"ทำความเข้าใจเรื่องนี้และนำวิธีไปใช้ได้", "เปรียบเทียบทางเลือก ค่าใช้จ่าย และข้อจำกัด", "ตรวจสอบเงื่อนไขก่อนลงมือทำ", "ค้นหาช่องทางทางการที่ถูกต้อง", "ตรวจสอบเงื่อนไขและการให้บริการในพื้นที่"},
		image:        "ภาพประกอบที่ระบุจุดสำคัญของ%s",
		factRules:    [2]string{"อ้างอิงแหล่งข้อมูลปฐมภูมิสำหรับตัวเลข วันที่ ข้อมูลจำเพาะ และราคา", "ตรวจสอบพื้นที่ วันที่มีผล และความขัดแย้งระหว่างแหล่งข้อมูล"},
	},
	"my": {
		title: "%s အတွက် လက်တွေ့လမ်းညွှန်", description: "%s အတွက် စစ်ဆေးရမည့် အချက်များ၊ လုပ်ဆောင်ပုံနှင့် ရလဒ်စစ်ဆေးနည်းကို ရှင်းပြထားပါသည်။",
		headings:     [3]string{"%s အတွက် စစ်ဆေးရမည့် အချက်များ", "%s အတွက် လုပ်ဆောင်ရမည့် အဆင့်များ", "ရလဒ်စစ်ဆေးနည်း"},
		questions:    [2]string{"%s မစတင်မီ ဘာတွေ စစ်ဆေးရမလဲ။", "ရလဒ် မှန်ကန်ကြောင်း ဘယ်လို အတည်ပြုမလဲ။"},
		intentAngles: [4]string{"%s အတွက် ရွေးချယ်စရာများကို နှိုင်းယှဉ်နည်း", "%s မလုပ်ဆောင်မီ ပြင်ဆင်ရမည့်အချက်များ", "%s အတွက် တရားဝင် ဝင်ပေါက်ကို ရှာဖွေနည်း", "%s အတွက် ဒေသအလိုက် စစ်ဆေးရမည့် သတ်မှတ်ချက်များ"},
		needs:        [5]string{"အကြောင်းအရာကို နားလည်ပြီး လက်တွေ့ အသုံးချလိုသည်။", "ရွေးချယ်စရာများ၏ ကုန်ကျစရိတ်နှင့် ကန့်သတ်ချက်များကို နှိုင်းယှဉ်လိုသည်။", "မလုပ်ဆောင်မီ သတ်မှတ်ချက်များကို စစ်ဆေးလိုသည်။", "တရားဝင် ဝင်ပေါက်ကို ရှာလိုသည်။", "ဒေသဆိုင်ရာ သတ်မှတ်ချက်နှင့် ဝန်ဆောင်မှုကို သိလိုသည်။"},
		image:        "%s ၏ အရေးကြီးသည့် အပိုင်းကို အမှတ်အသားပြထားသောပုံ",
		factRules:    [2]string{"ကိန်းဂဏန်း၊ ရက်စွဲ၊ သတ်မှတ်ချက်နှင့် ဈေးနှုန်းများအတွက် မူရင်းရင်းမြစ်ကို ကိုးကားပါ။", "သက်ဆိုင်ရာ ဒေသ၊ စတင်သက်ရောက်သည့် ရက်နှင့် ရင်းမြစ်များ၏ ကွဲလွဲချက်ကို စစ်ဆေးပါ။"},
	},
	"km": {
		title: "មគ្គុទ្ទេសក៍អនុវត្តសម្រាប់%s", description: "ស្វែងយល់ពីចំណុចត្រូវពិនិត្យ ជំហានអនុវត្ត និងវិធីផ្ទៀងផ្ទាត់លទ្ធផលសម្រាប់%s។",
		headings:     [3]string{"ចំណុចត្រូវពិនិត្យសម្រាប់%s", "ជំហានអនុវត្តសម្រាប់%s", "វិធីផ្ទៀងផ្ទាត់លទ្ធផល"},
		questions:    [2]string{"តើត្រូវពិនិត្យអ្វីខ្លះមុនពេលចាប់ផ្តើម%s?", "តើត្រូវផ្ទៀងផ្ទាត់លទ្ធផលដោយរបៀបណា?"},
		intentAngles: [4]string{"វិធីប្រៀបធៀបជម្រើសសម្រាប់%s", "អ្វីដែលត្រូវត្រៀមមុនអនុវត្ត%s", "វិធីស្វែងរកច្រកផ្លូវការសម្រាប់%s", "ចំណុចត្រូវពិនិត្យតាមមូលដ្ឋានសម្រាប់%s"},
		needs:        [5]string{"យល់អំពីប្រធានបទ និងអាចអនុវត្តបាន។", "ប្រៀបធៀបជម្រើស តម្លៃ និងដែនកំណត់។", "ពិនិត្យលក្ខខណ្ឌមុនពេលអនុវត្ត។", "ស្វែងរកច្រកផ្លូវការដែលត្រឹមត្រូវ។", "ពិនិត្យសេវាកម្ម និងលក្ខខណ្ឌតាមមូលដ្ឋាន។"},
		image:        "រូបភាពបង្ហាញចំណុចសំខាន់សម្រាប់%s",
		factRules:    [2]string{"យោងប្រភពដើមសម្រាប់តួលេខ កាលបរិច្ឆេទ លក្ខណៈបច្ចេកទេស និងតម្លៃ។", "ពិនិត្យតំបន់ កាលបរិច្ឆេទអនុវត្ត និងព័ត៌មានដែលផ្ទុយគ្នា។"},
	},
}

func intentPosition(intent string) int {
	switch intent {
	case "commercial":
		return 1
	case "transactional":
		return 2
	case "navigational":
		return 3
	case "local":
		return 4
	}
	return 0
}

func firstPartyKind(kind string) bool {
	switch kind {
	case "official", "government", "primary", "authorized":
		return true
	}
	return false
}

// Brief builds a locale-specific authoring brief using the project market and
// its saved evidence. Section headings describe work for the writer to complete.
func Brief(w domain.ContentWorkspace) (domain.ContentBrief, error) {
	if !hasResearch(w) {
		return domain.ContentBrief{}, domain.ErrContentPrerequisite
	}
	locale, ok := briefLocales[languageCode(w.Project.Market.LanguageCode)]
	if !ok {
		return domain.ContentBrief{}, &domain.ValidationError{Field: "language_code", Message: "a content brief is supported for en, zh, ja, th, my, and km"}
	}
	strategy := w.Strategy
	if strategy == nil {
		derived, err := Strategy(w)
		if err != nil {
			return domain.ContentBrief{}, err
		}
		strategy = &derived
	}
	if strategy.Action == "hold" || !sitePage(w.SiteURL, strategy.TargetURL) {
		return domain.ContentBrief{}, domain.ErrContentPrerequisite
	}
	e, keyword := w.Evidence, w.Project.Keyword
	intent := searchIntent(e.Keyword)
	position := intentPosition(intent.Primary)
	lead := locale.headings[0]
	if position > 0 {
		lead = locale.intentAngles[position-1]
	}
	brief := domain.ContentBrief{
		PrimaryKeyword: keyword, Secondary: []string{}, Intent: intent.Primary,
		Country: w.Project.Market.CountryCode, Language: w.Project.Market.LanguageCode,
		Need: locale.needs[position], PageType: strategy.PageType, URL: strategy.TargetURL,
		Title: bounded(fmt.Sprintf(locale.title, keyword), 76), Description: bounded(fmt.Sprintf(locale.description, keyword), 170),
		H1: bounded(keyword, 130), Topics: []string{keyword},
		Headings: []domain.ContentHeading{
			{Level: 2, Heading: fmt.Sprintf(lead, keyword), Purpose: locale.needs[position]},
			{Level: 2, Heading: fmt.Sprintf(locale.headings[1], keyword)},
			{Level: 3, Heading: locale.headings[2]},
		},
		Questions: []string{fmt.Sprintf(locale.questions[0], keyword), locale.questions[1]}, Entities: []string{}, SourceURLs: []string{},
		Gaps: []string{}, Images: []string{bounded(fmt.Sprintf(locale.image, keyword), 130)},
		InternalLinks:    matchingLinks(w.SiteURL, strategy.TargetURL, keyword, w.Project.Market.LanguageCode, ownedAssets(w)),
		FactRequirements: locale.factRules[:],
	}
	for _, cluster := range e.Clusters {
		if clusterContains(cluster, e.Keyword) {
			brief.Secondary = appendUnique(brief.Secondary, 12, append([]string{cluster.PrimaryKeyword}, cluster.Secondary...)...)
			brief.Secondary = appendUnique(brief.Secondary, 12, cluster.LongTail...)
		}
	}
	filtered := brief.Secondary[:0]
	for _, term := range brief.Secondary {
		if domain.KeywordKey(term) != domain.KeywordKey(keyword) {
			filtered = append(filtered, term)
		}
	}
	brief.Secondary = filtered
	for _, source := range w.Sources {
		if firstPartyKind(source.Kind) && canonicalKey(source.URL) != "" {
			brief.SourceURLs = appendUnique(brief.SourceURLs, 20, source.URL)
		}
	}
	for _, competitor := range e.Competitors {
		brief.Gaps = appendUnique(brief.Gaps, 16, competitor.Gaps...)
		for _, entity := range competitor.Page.Entities {
			if bounded(entity, 80) == entity {
				brief.Entities = appendUnique(brief.Entities, 20, entity)
			}
		}
		for _, topic := range competitor.Page.Topics {
			if bounded(topic, 70) == topic {
				brief.Topics = appendUnique(brief.Topics, 16, topic)
			}
		}
	}
	return brief, nil
}
