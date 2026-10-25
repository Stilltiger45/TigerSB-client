package xrayconf

// BasicDirectDomains and BasicDirectIPs go direct in the basic mode: every
// Russian site, by its domain zone (.ru and .рф) or by a Russian IP.
var (
	BasicDirectDomains = []string{"domain:ru", "domain:xn--p1ai"}
	BasicDirectIPs     = []string{"geoip:ru"}
)

// BuiltinDirectDomains are Russian sites that often break behind any VPN:
// universities. They go direct unless the profile sets no_builtin_direct or
// the mode is ModeAll. The panel extends the list through routing.direct.
var BuiltinDirectDomains = []string{
	"domain:edu.ru", "domain:msu.ru", "domain:spbu.ru", "domain:hse.ru",
	"domain:mipt.ru", "domain:bmstu.ru", "domain:itmo.ru", "domain:urfu.ru",
	"domain:kpfu.ru", "domain:nsu.ru", "domain:tpu.ru", "domain:misis.ru",
	"domain:mephi.ru", "domain:rudn.ru", "domain:ranepa.ru", "domain:fa.ru",
}

// ShieldDomains are services that must never see the tunnel: MAX, VK,
// Yandex, banks and government services. They watch for VPNs, and some
// refuse to work behind one. They always go direct, in every mode, and the
// panel cannot turn this off. ShieldApps are their Android apps, which are
// left out of the VPN altogether.
var ShieldDomains = []string{
	// MAX.
	"domain:max.ru", "domain:oneme.ru",
	// VK, Odnoklassniki, Mail.ru, Dzen, RuStore.
	"domain:vk.com", "domain:vk.ru", "domain:vk.me", "domain:vk.cc", "domain:vk.link",
	"domain:vkontakte.ru", "domain:userapi.com", "domain:vk-cdn.net", "domain:vkuser.net",
	"domain:vkuseraudio.net", "domain:vkuservideo.net", "domain:vk-portal.net",
	"domain:vkvideo.ru", "domain:vkplay.ru", "domain:vkplay.live", "domain:sferum.ru",
	"domain:mycdn.me", "domain:ok.ru", "domain:odnoklassniki.ru", "domain:okcdn.ru",
	"domain:mail.ru", "domain:imgsmail.ru", "domain:mradx.net",
	"domain:dzen.ru", "domain:rustore.ru",
	// Yandex.
	"domain:yandex.ru", "domain:yandex.net", "domain:yandex.com", "domain:yandex.by",
	"domain:yandex.kz", "domain:ya.ru", "domain:yastatic.net", "domain:yandex.cloud",
	"domain:yandexcloud.net", "domain:kinopoisk.ru", "domain:auto.ru", "domain:turbopages.org",
	// Banks and payments.
	"domain:sberbank.ru", "domain:sber.ru", "domain:sberbank.com",
	"domain:tbank.ru", "domain:tinkoff.ru", "domain:cdn-tinkoff.ru",
	"domain:vtb.ru", "domain:alfabank.ru", "domain:gazprombank.ru",
	"domain:raiffeisen.ru", "domain:sovcombank.ru", "domain:pochtabank.ru",
	"domain:open.ru", "domain:psbank.ru", "domain:rshb.ru", "domain:mkb.ru",
	"domain:domrf.ru", "domain:rosbank.ru", "domain:mtsbank.ru", "domain:uralsib.ru",
	"domain:akbars.ru", "domain:bspb.ru", "domain:rencredit.ru", "domain:otpbank.ru",
	"domain:ozonbank.ru", "domain:nspk.ru", "domain:mironline.ru", "domain:cbr.ru",
	"domain:yoomoney.ru", "domain:qiwi.com",
	// Government services.
	"domain:gosuslugi.ru", "domain:gu-st.ru", "domain:gov.ru", "domain:nalog.ru",
	"domain:mos.ru", "domain:emias.info", "domain:pochta.ru", "domain:rzd.ru",
}

// ShieldApps are the Android packages of ShieldDomains' services. Packages
// that are not installed are skipped.
var ShieldApps = []string{
	// MAX.
	"ru.oneme.app",
	// VK, Odnoklassniki, Mail.ru, Dzen, RuStore.
	"com.vkontakte.android", "com.vk.im", "com.vk.vkvideo", "com.uma.musicvk",
	"ru.ok.android", "ru.mail.mailapp", "ru.mail.cloud", "ru.zen.android", "ru.vk.store",
	// Yandex.
	"ru.yandex.searchplugin", "com.yandex.browser", "ru.yandex.yandexmaps",
	"ru.yandex.yandexnavi", "ru.yandex.taxi", "ru.yandex.music", "ru.kinopoisk",
	"ru.yandex.disk", "ru.yandex.mail", "ru.beru.android", "ru.foodfox.client",
	"com.yandex.bank", "ru.yandex.weatherplugin", "com.yandex.messenger",
	// Banks and payments.
	"ru.sberbankmobile", "ru.sberbank.sberbankid", "com.idamob.tinkoff.android",
	"ru.vtb24.mobilebanking.android", "ru.alfabank.mobile.android",
	"ru.gazprombank.android.mobilebank.app", "ru.raiffeisennews", "ru.sovcomcard.halva",
	"ru.pochtabank.pochtaapp", "logo.com.mbanking", "ru.rshb.dbo", "ru.mkb.mobile",
	"ru.rosbank.android", "ru.mts.money", "ru.ozon.fintech.finance", "ru.yoo.money",
	"ru.nspk.mirpay", "ru.akbars.mobile", "com.bssys.mbcphone",
	// Government services.
	"ru.rostel", "ru.gosuslugi.goskey", "ru.fns.lkfl", "ru.altarix.mos.pgu",
	"ru.russianpost.mobileapp", "ru.emias.app", "ru.rzd.pass",
}

// ForeignOnlyDomains are services that refuse Russian users: AI chats and
// tools. Their front pages often open from Russia, so a check of the site
// (ModeAuto) would send them direct or through the RU exit and the service
// would then refuse to work. They always go through the foreign exit when
// the profile has one, in every mode but off.
var ForeignOnlyDomains = []string{
	// OpenAI.
	"domain:openai.com", "domain:chatgpt.com", "domain:chat.com", "domain:sora.com",
	"domain:oaistatic.com", "domain:oaiusercontent.com",
	// Anthropic.
	"domain:anthropic.com", "domain:claude.ai", "domain:claude.com", "domain:clau.de",
	// Google Gemini, AI Studio, NotebookLM.
	"domain:gemini.google.com", "domain:gemini.google", "domain:bard.google.com",
	"domain:aistudio.google.com", "domain:ai.google.dev", "domain:makersuite.google.com",
	"domain:generativelanguage.googleapis.com", "domain:alkalimakersuite-pa.clients6.google.com",
	"domain:proactivebackend-pa.googleapis.com", "domain:robinfrontend-pa.googleapis.com",
	"domain:notebooklm.google", "domain:notebooklm.google.com", "domain:labs.google",
	"domain:deepmind.google", "domain:deepmind.com",
	// Microsoft and GitHub Copilot.
	"domain:copilot.microsoft.com", "domain:copilot.cloud.microsoft", "domain:githubcopilot.com",
	// xAI, Meta, Perplexity, Mistral and others.
	"domain:x.ai", "domain:grok.com", "domain:meta.ai", "domain:perplexity.ai", "domain:pplx.ai",
	"domain:mistral.ai", "domain:poe.com", "domain:character.ai", "domain:openrouter.ai",
	"domain:groq.com", "domain:cohere.com", "domain:midjourney.com", "domain:ideogram.ai",
	"domain:elevenlabs.io", "domain:suno.com", "domain:suno.ai", "domain:runwayml.com",
	"domain:cursor.com", "domain:cursor.sh", "domain:cursorapi.com",
	"domain:windsurf.com", "domain:codeium.com",
}
