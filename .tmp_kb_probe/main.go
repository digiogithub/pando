package main

import (
  "context"
  "fmt"
  "time"

  "github.com/digiogithub/pando/internal/config"
  "github.com/digiogithub/pando/internal/db"
  "github.com/digiogithub/pando/internal/rag"
)

func main() {
  cwd := "/www/MCP/Pando/pando"
  cfg, err := config.Load(cwd, false, "")
  if err != nil { panic(err) }
  conn, err := db.Connect()
  if err != nil { panic(err) }
  defer conn.Close()

  svc, err := rag.NewRemembrancesService(conn, &cfg.Remembrances)
  if err != nil { panic(err) }

  ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
  defer cancel()
  stats, err := svc.KB.SyncDirectoryWithStats(ctx, "/tmp/pando-kb-mini", true)
  fmt.Printf("stats=%+v err=%v\n", stats, err)
}
