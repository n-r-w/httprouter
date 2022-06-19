package httprouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/thanhpk/randstr"
	"github.com/n-r-w/lg"
	"github.com/n-r-w/nerr"
	"github.com/n-r-w/tools"
	"golang.org/x/exp/slices"
)

// Тип для описания ключевых значений параметров, добавляемых в контекст запроса
// в процессе его обработки через middleware
type contextKey string

const (
	// Ключ для хранения информации о сессии со стороны пользователя
	sessionName = "logserver"
	// Ключ для хранения id пользователя в сессии (в куках)
	userIDKeyName = "user_id"
	// Ключ для хранения номера сессии в контексте запроса
	ctxKeyRequestID = contextKey("request-id")
)

// Реализует интерфейс http.ResponseWriter
// Подменяет собой стандартный http.ResponseWriter и позволяет дополнительно сохранить в нем ошибку
type responseWriterEx struct {
	http.ResponseWriter
	code int
	err  error
}

func (w *responseWriterEx) WriteHeader(statusCode int) {
	if statusCode <= 0 {
		panic(nerr.New("invalid status code"))
	}
	w.code = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriterEx) WriteHeaderAndData(statusCode int, data []byte) (int, error) {
	if statusCode <= 0 {
		panic(nerr.New("invalid status code"))
	}
	w.code = statusCode
	w.ResponseWriter.WriteHeader(statusCode)

	if data == nil {
		return 0, nil
	}
	return w.Write(data)
}

// RouterData - реализует интерфейс Router
type RouterData struct {
	mux          *mux.Router
	sessionStore sessions.Store // Управление сессиями пользователей
	logger       lg.Logger

	subrouters map[string]*mux.Router
}

func New(logger lg.Logger) *RouterData {
	r := &RouterData{
		mux:          mux.NewRouter(),
		sessionStore: sessions.NewCookieStore([]byte(randstr.Hex(32))),
		logger:       logger,
		subrouters:   make(map[string]*mux.Router),
	}

	// подмешивание номера сессии
	r.mux.Use(r.setRequestID)
	// журналирование запросов
	r.mux.Use(r.logRequest)

	// разрешаем запросы к серверу c любых доменов (cross-origin resource sharing)
	r.mux.Use(handlers.CORS(handlers.AllowedOrigins([]string{"*"})))

	return r
}

func (router *RouterData) Handler() http.Handler {
	return router.mux
}

// RespondError Ответ с ошибкой
func (router *RouterData) RespondError(w http.ResponseWriter, code int, err error) {
	rw, ok := w.(*responseWriterEx)
	if !ok {
		panic("internal error")
	}

	rw.err = err
	router.RespondData(rw, code, "application/json; charset=utf-8", map[string]string{"error": err.Error()})
}

// RespondData Ответ на запрос без сжатия
func (router *RouterData) RespondData(w http.ResponseWriter, code int, contentType string, data interface{}) {
	rw, ok := w.(*responseWriterEx)
	if !ok {
		panic("internal error")
	}

	var err error

	if data == nil {
		_, err = rw.WriteHeaderAndData(code, nil)

	} else {
		if strings.Contains(contentType, "application/json") {
			if len(contentType) > 0 {
				w.Header().Set("Content-Type", contentType)
			}
			if jData, err1 := json.Marshal(data); err1 != nil {
				_, err = rw.WriteHeaderAndData(http.StatusInternalServerError, []byte(fmt.Sprintf(`{"error": "%v"}`, err1)))
			} else {
				_, err = rw.WriteHeaderAndData(code, jData)
			}
		} else {
			switch d := data.(type) {
			case string:
				if len(contentType) > 0 {
					w.Header().Set("Content-Type", contentType)
				}
				_, err = rw.WriteHeaderAndData(code, []byte(d))
			case []byte:
				if len(contentType) > 0 {
					w.Header().Set("Content-Type", contentType)
				}
				_, err = rw.WriteHeaderAndData(code, d)
			default:
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, err = rw.WriteHeaderAndData(http.StatusInternalServerError, []byte("unknown data type"))
			}
		}
	}

	if err != nil {
		router.logger.Err(err)
	}
}

// RespondCompressed Ответ на запрос со сжатием если его поддерживает клиент
func (router *RouterData) RespondCompressed(w http.ResponseWriter, r *http.Request, code int, compType CompressionType, contentType string, data interface{}) {
	if data == nil {
		router.RespondData(w, code, contentType, nil)

		return
	}

	rw, ok := w.(*responseWriterEx)
	if !ok {
		panic("internal error")
	}

	// проверяем хочет ли клиент сжатие
	compressionType := CompressionNo

	accepted := strings.Split(r.Header.Get("Accept-Encoding"), ",")
	if slices.Contains(accepted, "gzip") && compType == CompressionGzip {
		compressionType = CompressionGzip
	} else if slices.Contains(accepted, "deflate") && compType == CompressionDeflate {
		compressionType = CompressionDeflate
	}

	if compressionType == CompressionNo {
		router.RespondData(w, code, contentType, data)
		return
	}

	// заполняем буфер для сжатия
	var sourceData []byte
	switch d := data.(type) {
	case string:
	case []byte:
		sourceData = []byte(d)
	default:
		var errJSON error
		sourceData, errJSON = json.Marshal(data)

		if errJSON != nil {
			router.RespondError(w, http.StatusInternalServerError, errJSON)
			return
		}
	}

	if compressionType == CompressionGzip {
		rw.Header().Set("Content-Encoding", "gzip")
	} else {
		rw.Header().Set("Content-Encoding", "deflate")
	}

	compressedData, err := tools.CompressData(compressionType == CompressionDeflate, sourceData)

	if err != nil {
		router.RespondError(w, http.StatusInternalServerError, err)
		return
	}

	rw.Header().Set("Content-Type", contentType)
	_, _ = rw.WriteHeaderAndData(code, compressedData)
}

// AddRoute ...
func (router *RouterData) AddRoute(subroute string, route string, handler http.HandlerFunc, methods ...string) {
	var r *mux.Router
	if len(subroute) == 0 {
		r = router.mux
	} else {
		r = router.getSubrouter(subroute)
	}

	r.HandleFunc(route, handler).Methods(methods...)
}

// AddMiddleware ...
func (router *RouterData) AddMiddleware(subroute string, mwf ...MiddlewareFunc) {
	funcs := make([]mux.MiddlewareFunc, len(mwf))
	for i, f := range mwf {
		funcs[i] = func(h http.Handler) http.Handler { return f(h) }
	}

	if len(subroute) == 0 {
		router.mux.Use(funcs...)
	} else {
		router.getSubrouter(subroute).Use(funcs...)
	}
}

// StartSession ...
func (router *RouterData) StartSession(w http.ResponseWriter, r *http.Request, userID uint64, sessionAge int) error {
	// получаем сесиию
	session, err := router.sessionStore.New(r, sessionName)
	if err != nil {
		return err
	}

	// записываем информацию о том, что пользователь с таким ID залогинился
	session.Values[userIDKeyName] = userID
	session.Options = &sessions.Options{
		Path:   "/",
		Domain: "",
		MaxAge: int(sessionAge),
		Secure: false,
		// HttpOnly: true, // прячем содержимое сессии от доступа через JavaSript в браузере
		HttpOnly: false,
		SameSite: 0,
	}

	return router.sessionStore.Save(r, w, session)
}

func (router *RouterData) CheckSession(r *http.Request) (userID uint64, err error) {
	// извлекаем из запроса пользователя куки с информацией о сессии
	session, err := router.sessionStore.Get(r, sessionName)
	if err != nil {
		return 0, err
	}

	// ищем в информацию о пользователе в сессиях
	ID, ok := session.Values[userIDKeyName]
	if !ok || session.Options.MaxAge < 0 {
		return 0, nerr.New("unauthorized")
	}

	return ID.(uint64), nil
}

func (router *RouterData) CloseSession(w http.ResponseWriter, r *http.Request) {
	// получаем сесиию
	session, err := router.sessionStore.Get(r, sessionName)
	if err != nil {
		router.logger.Error("session store get error %v", err)

		return
	}
	if session == nil {
		return
	}

	// удаляем из нее данные о логине
	delete(session.Values, userIDKeyName)
	// сохраняем
	if err := router.sessionStore.Save(r, w, session); err != nil {
		router.logger.Error("session save error")
	}
}

func (router *RouterData) getSubrouter(path string) *mux.Router {
	sr := router.subrouters[path]
	if sr == nil {
		sr = router.mux.PathPrefix(path).Subrouter()
		router.subrouters[path] = sr
	}

	return sr
}

// Добавляем к контексту уникальный ID сесии с ключом ctxKeyRequestID
func (router *RouterData) setRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.New().String()
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, id)))
	})
}

// Выводим запросы в лог
func (router *RouterData) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriterEx{
			ResponseWriter: w,
			code:           http.StatusOK,
			err:            nil,
		}

		// вызываем обработчик нижнего уровня
		next.ServeHTTP(rw, r)

		// выводим в журнал результат
		var level lg.Level
		switch {
		case rw.code >= http.StatusInternalServerError:
			level = lg.Error
		case rw.code >= http.StatusBadRequest:
			level = lg.Warn
		default:
			level = lg.Info
		}

		var errorText string
		if rw.err != nil {
			errorText = rw.err.Error()
			errorText = strings.ReplaceAll(errorText, `"`, "")
		} else {
			errorText = "-"
		}

		if level == lg.Error || level == lg.Warn {
			router.logger.Level(level, "addr: %s, completed with %d %s in %v, %s",
				r.RemoteAddr,
				rw.code,
				http.StatusText(rw.code),
				time.Since(start),
				errorText)
		}
	})
}
