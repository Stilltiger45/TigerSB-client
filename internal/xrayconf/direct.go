package xrayconf

// BasicDirectDomains and BasicDirectIPs go direct in the basic mode: every
// Russian site, by its domain zone (.ru and .рф) or by a Russian IP.
var (
	BasicDirectDomains = []string{"domain:ru", "domain:xn--p1ai"}
	BasicDirectIPs     = []string{"geoip:ru"}
)

// BuiltinDirectDomains are Russian sites that often break behind any VPN:
// banks, payment systems, government services and universities. They always go
// direct unless the profile sets no_builtin_direct. The panel extends the list
// through routing.direct.
var BuiltinDirectDomains = []string{
	// Banks and payments.
	"domain:sberbank.ru", "domain:sber.ru", "domain:sberbank.com",
	"domain:tbank.ru", "domain:tinkoff.ru",
	"domain:vtb.ru", "domain:alfabank.ru", "domain:gazprombank.ru",
	"domain:raiffeisen.ru", "domain:sovcombank.ru", "domain:pochtabank.ru",
	"domain:open.ru", "domain:psbank.ru", "domain:rshb.ru", "domain:mkb.ru",
	"domain:domrf.ru", "domain:rosbank.ru", "domain:mtsbank.ru",
	"domain:nspk.ru", "domain:mironline.ru", "domain:sbp.nspk.ru",
	"domain:yoomoney.ru", "domain:qiwi.com",
	// Government services.
	"domain:gosuslugi.ru", "domain:gov.ru", "domain:nalog.ru", "domain:mos.ru",
	"domain:esia.gosuslugi.ru", "domain:pfr.gov.ru", "domain:sfr.gov.ru",
	// Universities.
	"domain:edu.ru", "domain:msu.ru", "domain:spbu.ru", "domain:hse.ru",
	"domain:mipt.ru", "domain:bmstu.ru", "domain:itmo.ru", "domain:urfu.ru",
	"domain:kpfu.ru", "domain:nsu.ru", "domain:tpu.ru", "domain:misis.ru",
	"domain:mephi.ru", "domain:rudn.ru", "domain:ranepa.ru", "domain:fa.ru",
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
