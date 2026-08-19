// Package mailcfg resolves IMAP/SMTP hosts from a mailbox address.
package mailcfg

import "strings"

func domainOf(address string) string {
	addr := strings.ToLower(strings.TrimSpace(address))
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return ""
	}
	return addr[at+1:]
}

func provider(domain string) string {
	switch domain {
	case "yandex.ru", "yandex.com", "ya.ru", "yandex.by", "yandex.kz":
		return "yandex"
	case "mail.ru", "inbox.ru", "bk.ru", "list.ru", "internet.ru":
		return "mailru"
	case "gmail.com", "googlemail.com":
		return "gmail"
	default:
		return ""
	}
}

// IMAPHost returns IMAP host and port (993) for the mailbox domain.
func IMAPHost(address string) (host string, port int) {
	port = 993
	domain := domainOf(address)
	switch provider(domain) {
	case "yandex":
		return "imap.yandex.ru", port
	case "mailru":
		return "imap.mail.ru", port
	case "gmail":
		return "imap.gmail.com", port
	default:
		if domain != "" {
			return "imap." + domain, port
		}
		return "imap.yandex.ru", port
	}
}

// SMTPHost returns SMTP host and port (587 STARTTLS) for the mailbox domain.
func SMTPHost(address string) (host string, port int) {
	port = 587
	domain := domainOf(address)
	switch provider(domain) {
	case "yandex":
		return "smtp.yandex.ru", port
	case "mailru":
		return "smtp.mail.ru", port
	case "gmail":
		return "smtp.gmail.com", port
	default:
		if domain != "" {
			return "smtp." + domain, port
		}
		return "smtp.yandex.ru", port
	}
}
