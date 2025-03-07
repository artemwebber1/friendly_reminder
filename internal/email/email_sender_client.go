package email

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/smtp"

	"github.com/artemwebber1/friendly_reminder/internal/repository"
)

type EmailSenderClient struct {
	from     string
	password string
	host     string
	port     string
	auth     smtp.Auth

	usersRepo repository.UsersRepository
	itemsRepo repository.ItemsRepository
}

func NewSender(from, password, host, port string, ur repository.UsersRepository, ir repository.ItemsRepository) *EmailSenderClient {
	return &EmailSenderClient{
		from:      from,
		password:  password,
		host:      host,
		port:      port,
		auth:      smtp.PlainAuth("", from, password, host),
		usersRepo: ur,
		itemsRepo: ir,
	}
}

func (s *EmailSenderClient) Send(subject, body, to string) error {
	msg := fmt.Appendf(
		nil,
		"To: %s\r\n"+
			"Subject: %s\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Content-Type: text/plain; charset=\"UTF-8\"\r\n"+
			"Content-Transfer-Encoding: base64\r\n\r\n"+
			"%s",
		to, subject, base64.StdEncoding.EncodeToString([]byte(body)))

	addr := s.host + ":" + s.port
	err := smtp.SendMail(
		addr,
		s.auth,
		s.from,
		[]string{to},
		msg,
	)

	log.Printf("Sent email from '%s' to '%s'\n", s.from, to)

	return err
}
