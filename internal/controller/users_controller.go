package controller

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/artemwebber1/friendly_reminder/internal/config"
	"github.com/artemwebber1/friendly_reminder/internal/hasher"
	mw "github.com/artemwebber1/friendly_reminder/internal/middleware"
	"github.com/artemwebber1/friendly_reminder/internal/models"
	"github.com/artemwebber1/friendly_reminder/pkg/email"
	"github.com/artemwebber1/friendly_reminder/pkg/jwtservice"
	"github.com/golang-jwt/jwt/v5"
)

type usersRepository interface {
	// AddUser добавляет нового пользователя.
	//
	// Возвращает id нового пользователя и ошибку.
	AddUser(ctx context.Context, email, password string) (int64, error)

	// DeleteUser удаляет пользователя из базы данных.
	DeleteUser(ctx context.Context, email string) error

	// Subscribe подписывает пользователя на рассылку электронных писем.
	// Если параметр subscribe = true, пользователь будет подписан на рассылку, иначе будет отписан.
	Subscribe(ctx context.Context, email string, subscr bool) error

	// GetEmailsSubscribed возвращает список зарегестрированных электронных почт пользователей, подписанных на рассылку.
	GetEmailsSubscribed(ctx context.Context) ([]string, error)

	// EmailExists возвращает true если пользователь с данной электронной почтой уже существует.
	EmailExists(ctx context.Context, email string) bool

	// UserExists возвращает true, если существует пользователь с указанной почтой и паролем.
	UserExists(ctx context.Context, email, password string) bool
}

// unverifiedUsersRepository является репозиторием неверифицированных пользователей.
//
// Неверифицированный пользователь - это пользователь, который
// регистрировался в системе, но не подтвердил электронную почту.
type unverifiedUsersRepository interface {
	// TokenExists возвращает true если существует указанный токен для подтверждения электронной почты.
	TokenExists(t string) bool

	// CreateToken добавляет пользователя в базу данных, как не подтвердившего электронную почту, и создаёт токен для подтверждения.
	// Возвращает сам токен и ошибку.
	CreateToken(email, pwd string) (string, error)

	// DeleteToken удаляет токен из базы данных.
	DeleteToken(token string) error

	// UpdateToken создаёт новый токен для пользователя с указанным email.
	UpdateToken(email string) (string, error)

	// HasToken возвращает true, если для указанной электронной почты уже сгенерирован токен.
	HasToken(email string) bool

	// GetUserByToken получает пользователя по токену.
	GetUserByToken(token string) (models.User, error)
}

type UsersController struct {
	emailSender email.Sender
	cfg         *config.Config

	usersRepo           usersRepository
	unverifiedUsersRepo unverifiedUsersRepository
}

func NewUsersController(
	ur usersRepository,
	uur unverifiedUsersRepository,
	emailSender email.Sender,
	cfg *config.Config) *UsersController {
	return &UsersController{
		usersRepo:           ur,
		unverifiedUsersRepo: uur,
		emailSender:         emailSender,
		cfg:                 cfg,
	}
}

func (c *UsersController) AddEndpoints(mux *http.ServeMux) {
	mux.HandleFunc(
		"POST "+c.cfg.Prefix+"/users/new",
		mw.UseLogging(c.SendConfirmEmailLink),
	)

	mux.HandleFunc(
		"POST /api/v1/users/login",
		mw.UseLogging(c.Login),
	)

	mux.HandleFunc(
		"GET "+c.cfg.Prefix+"/users/confirm-email",
		mw.UseLogging(c.ConfirmEmail),
	)

	mux.HandleFunc(
		"PATCH "+c.cfg.Prefix+"/users/subscribe",
		mw.UseLogging(mw.UseAuthorization(c.SubscribeUser)),
	)
}

// AddUser создаёт нового пользователя в базе данных.
//
// Обрабатывает POST запросы по пути '/users/new'.
func (c *UsersController) SendConfirmEmailLink(w http.ResponseWriter, r *http.Request) {
	user, err := readBody[models.User](r.Body)
	if err != nil {
		http.Error(w, errReadingBody.Error(), http.StatusBadRequest)
	}

	if c.usersRepo.EmailExists(r.Context(), user.Email) {
		http.Error(w, "user with this email already exists", http.StatusForbidden)
		return
	}

	// Отправляем пользователю на почту ссылку для подтверждения электронной почты

	var confirmToken string
	if !c.unverifiedUsersRepo.HasToken(user.Email) {
		confirmToken, err = c.unverifiedUsersRepo.CreateToken(user.Email, hasher.Hash(user.Password))
	} else {
		confirmToken, err = c.unverifiedUsersRepo.UpdateToken(user.Email)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("error creating confirm token: %s", err), http.StatusInternalServerError)
		return
	}

	// Ссылка для подтверждения электронной почты
	confirmLink := c.cfg.Host + ":" + c.cfg.Port + c.cfg.Prefix + "/users/confirm-email?t=" + confirmToken

	log.Printf("Sending an email confirmation link to '%s'...\n", user.Email)

	const subject = "Email confirmation"
	body := fmt.Sprintf("Please, confirm your email by clicking on the link:\n%s\n\nIf you didn't request this mail, ignore it.", confirmLink)

	go c.emailSender.Send(
		subject,
		body,
		user.Email)

	w.Write([]byte(confirmToken))
}

// ConfirmEmail является эндпоинтом, на который пользователь попадёт, подтверждая электронную почту.
//
// Обрабатывает GET запросы по пути '/users/confirm-email'.
func (c *UsersController) ConfirmEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("t")
	if !c.unverifiedUsersRepo.TokenExists(token) {
		http.Error(w, "invalid confirm token", http.StatusForbidden)
		return
	}

	user, err := c.unverifiedUsersRepo.GetUserByToken(token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = c.unverifiedUsersRepo.DeleteToken(token)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to delete confirm token: %s", err), http.StatusForbidden)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("Email confirmed succesfully"))

	// Пользователь успешно подтвердил электронную почту, добавляем его в базу данных
	c.usersRepo.AddUser(r.Context(), user.Email, user.Password)
}

// SubscribeUser подписывает пользователя с указанным email на рассылку писем.
//
// Обрабатывает PATCH запросы по пути '/users/subscribe'.
func (c *UsersController) SubscribeUser(w http.ResponseWriter, r *http.Request) {
	rawJwt := jwtservice.FromHeader(r.Header)

	jwtClaims, err := jwtservice.GetClaims(rawJwt, jwtKey())
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	email, err := jwtClaims.GetSubject()
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	if !c.usersRepo.EmailExists(r.Context(), email) {
		http.Error(w, errInvalidInvalidEmail.Error(), http.StatusForbidden)
		return
	}

	subscribe, err := strconv.ParseBool(r.URL.Query().Get("subscribe"))
	if err != nil {
		http.Error(w, "invalid value for 'subscribe' param", http.StatusBadRequest)
		return
	}
	c.usersRepo.Subscribe(r.Context(), email, subscribe)
}

// Login осуществляет вход уже существующего пользователя в систему.
//
// Обрабатывает POST запросы по пути '/users/login'.
func (c *UsersController) Login(w http.ResponseWriter, r *http.Request) {
	user, err := readBody[models.User](r.Body)
	if err != nil {
		http.Error(w, errReadingBody.Error(), http.StatusBadRequest)
		return
	}

	if !c.usersRepo.UserExists(r.Context(), user.Email, hasher.Hash(user.Password)) {
		http.Error(w, "invalid email or password", http.StatusForbidden)
		return
	}

	// Создание jwt
	claims := jwt.MapClaims{
		"sub": user.Email,
		"exp": time.Now().Add(time.Hour).Unix(),
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokStr, err := tok.SignedString(jwtKey())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Write([]byte(tokStr))
}
