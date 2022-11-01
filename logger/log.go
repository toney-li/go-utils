package logger

import (
	"github.com/gin-gonic/gin"
	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"net"
	"net/http/httputil"
	"os"
	"runtime/debug"
	"strings"
	"time"
)

// 1 定义一下logger使用的常量
const (
	mode        = "dev"              //开发模式
	filename    = "web_app.log"      // 日志存放路径
	level       = zapcore.DebugLevel // 日志级别
	max_size    = 200                //最大存储大小
	max_age     = 30                 //最大存储时间
	max_backups = 7                  //#备份数量
)

// 2 初始化Logger对象
func InitLogger() (err error) {
	// 创建Core三大件，进行初始化
	writeSyncer := getLogWriter(filename, max_size, max_backups, max_age)
	encoder := getEncoder()
	// 创建核心-->如果是dev模式，就在控制台和文件都打印，否则就只写到文件中
	var core zapcore.Core
	if mode == "dev" {
		// 开发模式，日志输出到终端
		consoleEncoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
		// NewTee创建一个核心，将日志条目复制到两个或多个底层核心中。
		core = zapcore.NewTee(
			zapcore.NewCore(encoder, writeSyncer, level),
			zapcore.NewCore(consoleEncoder, zapcore.Lock(os.Stdout), level),
		)
	} else {
		core = zapcore.NewCore(encoder, writeSyncer, level)
	}

	//core := zapcore.NewCore(encoder, writeSyncer, level)
	// 创建 logger 对象
	log := zap.New(core, zap.AddCaller())
	// 替换全局的 logger, 后续在其他包中只需使用zap.L()调用即可
	zap.ReplaceGlobals(log)
	return
}

// 获取Encoder，给初始化logger使用的
func getEncoder() zapcore.Encoder {
	// 使用zap提供的 NewProductionEncoderConfig
	encoderConfig := zap.NewProductionEncoderConfig()
	// 设置时间格式
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	// 时间的key
	encoderConfig.TimeKey = "time"
	// 级别
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	// 显示调用者信息
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	// 返回json 格式的 日志编辑器
	return zapcore.NewJSONEncoder(encoderConfig)
}

// 获取切割的问题，给初始化logger使用的
func getLogWriter(filename string, maxSize, maxBackup, maxAge int) zapcore.WriteSyncer {
	// 使用 lumberjack 归档切片日志
	lumberJackLogger := &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    maxSize,
		MaxBackups: maxBackup,
		MaxAge:     maxAge,
	}
	return zapcore.AddSync(lumberJackLogger)
}

// GinLogger 用于替换gin框架的Logger中间件，不传参数，直接这样写
func GinLogger(c *gin.Context) {
	logger := zap.L()
	start := time.Now()
	path := c.Request.URL.Path
	query := c.Request.URL.RawQuery
	c.Next() // 执行视图函数
	// 视图函数执行完成，统计时间，记录日志
	cost := time.Since(start)
	logger.Info(path,
		zap.Int("status", c.Writer.Status()),
		zap.String("method", c.Request.Method),
		zap.String("path", path),
		zap.String("query", query),
		zap.String("ip", c.ClientIP()),
		zap.String("user-agent", c.Request.UserAgent()),
		zap.String("errors", c.Errors.ByType(gin.ErrorTypePrivate).String()),
		zap.Duration("cost", cost),
	)

}

// GinRecovery 用于替换gin框架的Recovery中间件，因为传入参数，再包一层
func GinRecovery(stack bool) gin.HandlerFunc {
	logger := zap.L()
	return func(c *gin.Context) {
		defer func() {
			// defer 延迟调用，出了异常，处理并恢复异常，记录日志
			if err := recover(); err != nil {
				//  这个不必须，检查是否存在断开的连接(broken pipe或者connection reset by peer)---------开始--------
				var brokenPipe bool
				if ne, ok := err.(*net.OpError); ok {
					if se, ok := ne.Err.(*os.SyscallError); ok {
						if strings.Contains(strings.ToLower(se.Error()), "broken pipe") || strings.Contains(strings.ToLower(se.Error()), "connection reset by peer") {
							brokenPipe = true
						}
					}
				}
				//httputil包预先准备好的DumpRequest方法
				httpRequest, _ := httputil.DumpRequest(c.Request, false)
				if brokenPipe {
					logger.Error(c.Request.URL.Path,
						zap.Any("error", err),
						zap.String("request", string(httpRequest)),
					)
					// 如果连接已断开，我们无法向其写入状态
					c.Error(err.(error))
					c.Abort()
					return
				}
				//  这个不必须，检查是否存在断开的连接(broken pipe或者connection reset by peer)---------结束--------

				// 是否打印堆栈信息，使用的是debug.Stack()，传入false，在日志中就没有堆栈信息
				if stack {
					logger.Error("[Recovery from panic]",
						zap.Any("error", err),
						zap.String("request", string(httpRequest)),
						zap.String("stack", string(debug.Stack())),
					)
				} else {
					logger.Error("[Recovery from panic]",
						zap.Any("error", err),
						zap.String("request", string(httpRequest)),
					)
				}
				// 有错误，直接返回给前端错误，前端直接报错
				//c.AbortWithStatus(http.StatusInternalServerError)
				// 该方式前端不报错
				c.String(200, "访问出错了")
			}
		}()
		c.Next()
	}
}

//import (
//	"bytes"
//	"fmt"
//	"github.com/gin-gonic/gin"
//	"github.com/sirupsen/logrus"
//	"os"
//	"path/filepath"
//)
//
//var Log = logrus.New() // 创建一个log示例
//
//func init() { // 初始化log的函数
//	loggerFile := os.Getenv("LOGGER_FILE")
//	if loggerFile == "" {
//		loggerFile = "./gotify.log"
//	}
//	Log.Formatter = &logrus.JSONFormatter{}                                      // 设置为json格式的日志
//	f, err := os.OpenFile(loggerFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644) // 创建一个log日志文件
//	if err != nil {
//		panic("Init log fail")
//	}
//	Log.Out = f                  // 设置log的默认文件输出
//	gin.SetMode(gin.ReleaseMode) // 线上模式，控制台不会打印信息
//	gin.DefaultWriter = Log.Out  // gin框架自己记录的日志也会输出
//	Log.Level = logrus.InfoLevel // 设置日志级别
//	return
//}
//
//type MyFormatter struct {
//}
//
//func (m *MyFormatter) Format(entry *logrus.Entry) ([]byte, error) {
//	var b *bytes.Buffer
//	if entry.Buffer != nil {
//		b = entry.Buffer
//	} else {
//		b = &bytes.Buffer{}
//	}
//	var newLog string
//	if entry.HasCaller() {
//		fName := filepath.Base(entry.Caller.File)
//		newLog = fmt.Sprintf(" %v | %s | %s %d %s | %s %#v\n%s",
//			entry.Time.Format("2006-01-02 15:04:05"),
//			entry.Level,
//			fName, entry.Caller.Line, entry.Caller.Function,
//			entry.Message,
//		)
//	} else {
//		newLog = fmt.Sprintf(" %v | %s | | %s %#v\n%s",
//			entry.Time.Format("2006-01-02 15:04:05"),
//			entry.Level,
//			entry.Message,
//		)
//	}
//
//	b.WriteString(newLog)
//	return b.Bytes(), nil
//}
