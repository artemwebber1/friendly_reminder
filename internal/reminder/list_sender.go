package reminder

import (
	"fmt"
	"log"
	"time"

	"github.com/artemwebber1/friendly_reminder/internal/repository"
	"github.com/artemwebber1/friendly_reminder/pkg/email"
)

// ListSender отправляет списки дел пользователям, используя интерфейс [email.Sender].
type ListSender struct {
	sender    email.Sender
	usersRepo repository.UsersRepository
	tasksRepo repository.TasksRepository
}

func New(s email.Sender, ur repository.UsersRepository, tr repository.TasksRepository) *ListSender {
	return &ListSender{
		sender:    s,
		usersRepo: ur,
		tasksRepo: tr,
	}
}

// StartSending в достаёт из базы данных электронные почты всех пользователей,
// подписанных на рассылку, и отправляет им их списки дел c указанным интервалом.
func (s *ListSender) StartSending(d time.Duration) {
	for {
		log.Println("Sending emails")
		emails, err := s.usersRepo.GetEmails()
		if err != nil {
			log.Fatal(err)
		}

		for _, email := range emails {
			go s.sendList(email)
		}

		time.Sleep(d)
	}
}

func (s *ListSender) sendList(email string) {
	// Получаем список пользователя
	userTasks, err := s.tasksRepo.GetList(email)
	if err != nil {
		log.Fatal(err)
	}

	// Преобразуем слайс list в строку вида:
	// 1. Задача 1
	// 2. Задача 2
	// ...
	listStr := ""
	for _, item := range userTasks {
		listStr += fmt.Sprintf("\n%d. %s", item.NumberInList, item.Value)
	}

	subject := "Ваш список дел"
	if len(userTasks) == 0 {
		// Отписываем пользователя от рассылки, если его список пуст, и информируем его об этом.
		subject = "Вы были отписаны от рассылки"
		listStr = "Ваш список дел пуст. Вы будете отписаны от рассылки, пока не добавите новые дела и не подпишетесь на рассылку снова."
		s.usersRepo.MakeSigned(email, false) // Отписка от рассылки
	}

	if err = s.sender.Send(subject, listStr, email); err != nil {
		log.Fatal(err)
	}
}
