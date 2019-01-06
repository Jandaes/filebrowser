package cmd

import (
	"crypto/tls"
	"errors"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/asdine/storm"
	"github.com/filebrowser/filebrowser/v2/auth"
	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/users"

	fbhttp "github.com/filebrowser/filebrowser/v2/http"
	"github.com/spf13/cobra"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

var (
	databasePath string
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&databasePath, "database", "d", "./filebrowser.db", "path to the database")

	rootCmd.Flags().StringP("address", "a", "127.0.0.1", "address to listen on")
	rootCmd.Flags().StringP("log", "l", "stdout", "log output")
	rootCmd.Flags().IntP("port", "p", 80, "port to listen on")
	rootCmd.Flags().StringP("cert", "c", "", "tls certificate")
	rootCmd.Flags().StringP("key", "k", "", "tls key")
	rootCmd.Flags().StringP("scope", "s", "", "root scope to which user's scope are relative too")
}

var rootCmd = &cobra.Command{
	Use:   "filebrowser",
	Short: "A stylish web-based file browser",
	Long: `File Browser CLI lets you create the database to use with File Browser,
manage your user and all the configurations without accessing the
web interface.

If you've never run File Browser, you will need to create the database.
See 'filebrowser help config init' for more information.`,
	Run: serveAndListen,
}

func serveAndListen(cmd *cobra.Command, args []string) {
	switch logMethod := mustGetString(cmd, "log"); logMethod {
	case "stdout":
		log.SetOutput(os.Stdout)
	case "stderr":
		log.SetOutput(os.Stderr)
	case "":
		log.SetOutput(ioutil.Discard)
	default:
		log.SetOutput(&lumberjack.Logger{
			Filename:   logMethod,
			MaxSize:    100,
			MaxAge:     14,
			MaxBackups: 10,
		})
	}

	if _, err := os.Stat(databasePath); os.IsNotExist(err) {
		quickSetup(cmd)
	}

	db := getDB()
	defer db.Close()
	st := getStorage(db)

	port := mustGetInt(cmd, "port")
	address := mustGetString(cmd, "address")
	cert := mustGetString(cmd, "cert")
	key := mustGetString(cmd, "key")

	handler, err := fbhttp.NewHandler(st)
	checkErr(err)

	var listener net.Listener

	if key != "" && cert != "" {
		cer, err := tls.LoadX509KeyPair(cert, key)
		checkErr(err)
		config := &tls.Config{Certificates: []tls.Certificate{cer}}
		listener, err = tls.Listen("tcp", address+":"+strconv.Itoa(port), config)
		checkErr(err)
	} else {
		listener, err = net.Listen("tcp", address+":"+strconv.Itoa(port))
		checkErr(err)
	}

	log.Println("Listening on", listener.Addr().String())
	if err := http.Serve(listener, handler); err != nil {
		log.Fatal(err)
	}
}

func quickSetup(cmd *cobra.Command) {
	scope := mustGetString(cmd, "scope")
	if scope == "" {
		panic(errors.New("scope flag must be set for quick setup"))
	}

	db, err := storm.Open(databasePath)
	checkErr(err)
	defer db.Close()

	set := &settings.Settings{
		Key:        generateRandomBytes(64), // 256 bit
		BaseURL:    "",
		Signup:     false,
		AuthMethod: auth.MethodJSONAuth,
		Defaults: settings.UserDefaults{
			Scope:  scope,
			Locale: "en",
			Perm: users.Permissions{
				Admin:    false,
				Execute:  true,
				Create:   true,
				Rename:   true,
				Modify:   true,
				Delete:   true,
				Share:    true,
				Download: true,
			},
		},
	}

	st := getStorage(db)

	err = st.Settings.Save(set)
	checkErr(err)

	err = st.Auth.Save(&auth.JSONAuth{})
	checkErr(err)

	password, err := users.HashPwd("admin")
	checkErr(err)

	user := &users.User{
		Username:     "admin",
		Password:     password,
		LockPassword: false,
	}

	set.Defaults.Apply(user)
	user.Perm.Admin = true

	err = st.Users.Save(user)
	checkErr(err)
}
