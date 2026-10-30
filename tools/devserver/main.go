// 开发用：只起后端服务，把地址与令牌打到 stdout，供浏览器直连调试。用法：go run ./tools/devserver -token dev
// 端口由服务自选（随机回环端口），启动后会把实际端口与令牌打出来。
// 它不嵌前端：前端用 npm run dev 起 Vite，再在浏览器控制台里设 localStorage 的 omp.dev.port / omp.dev.token 即可直连——改后端时编译一次、起一次服务、浏览器刷新就能看到效果。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"OhMyPiDesktop/backend/hub"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/repo"
	"OhMyPiDesktop/backend/server"
	"OhMyPiDesktop/backend/service"
)

func main() {
	token := flag.String("token", "dev", "启动令牌（前端要设同一个）")
	dataDir := flag.String("data", "", "数据目录（默认用系统临时目录下的一个固定路径）")
	flag.Parse()

	dir := *dataDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "oh-my-pi-dev")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Println("数据目录不可写:", err)
		os.Exit(1)
	}
	store, err := repo.Open(filepath.Join(dir, "dev.json"))
	must(err)
	defer store.Close()

	h := hub.New(1024)
	svc, err := service.New(service.Options{Store: store, Hub: h, DataDir: dir, Version: "dev"})
	must(err)

	srv := server.New(svc, h, *token)
	port, err := srv.Start()
	must(err)
	fmt.Printf("READY http://127.0.0.1:%d/api/v1 token=%s\n", port, *token)
	fmt.Printf("browser: localStorage.setItem('omp.dev.port','%d'); localStorage.setItem('omp.dev.token','%s')\n", port, *token)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	svc.Engine.StopAll()
	srv.Stop(ctx)
	pkg.Infof("devserver: 已退出")
}

// must 遇错就退出：开发工具的失败不需要优雅收尾。
func must(err error) {
	if err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}
}
