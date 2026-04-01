package main

import (
  "context"
  "fmt"
  "path/filepath"

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
  if svc == nil || svc.KB == nil { panic("no kb service") }

  kbPath := cfg.Remembrances.KBPath
  if !filepath.IsAbs(kbPath) { kbPath = filepath.Join(cwd, kbPath) }
  stats, err := svc.KB.SyncDirectoryWithStats(context.Background(), kbPath, true)
  fmt.Printf("stats=%+v err=%v\n", stats, err)
}
