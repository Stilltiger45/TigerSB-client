package xrayconf

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
