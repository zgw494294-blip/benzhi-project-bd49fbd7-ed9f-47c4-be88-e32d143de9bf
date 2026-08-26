package main

import (
	"aquaflush-release-workbench/internal/app"
	"aquaflush-release-workbench/internal/domain"
	"aquaflush-release-workbench/internal/httpui"
	"aquaflush-release-workbench/internal/store"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func main() {
	addrDefault := "127.0.0.1:19091"
	if p := os.Getenv("PORT"); p != "" {
		if n, e := strconv.Atoi(p); e == nil && n > 0 && n < 65536 {
			addrDefault = "127.0.0.1:" + p
		}
	}
	addr := flag.String("addr", addrDefault, "监听地址")
	self := flag.Bool("selfcheck", false, "运行自检")
	flag.Parse()
	if *self {
		if err := selfcheck(*addr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(*addr, ""); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(addr, dbPath string) error {
	if dbPath == "" {
		dbPath = filepath.Join(os.TempDir(), "aquaflush.db")
	}
	s, e := store.New(dbPath)
	if e != nil {
		return e
	}
	defer s.Close()
	srv := &http.Server{Addr: addr, Handler: httpui.New(app.New(s)).Routes()}
	return srv.ListenAndServe()
}
func selfcheck(addr string) error {
	db := filepath.Join(os.TempDir(), fmt.Sprintf("aquaflush-selfcheck-%d.db", time.Now().UnixNano()))
	s, e := store.New(db)
	if e != nil {
		return e
	}
	defer s.Close()
	h := httpui.New(app.New(s))
	srv := &http.Server{Addr: addr, Handler: h.Routes()}
	go srv.ListenAndServe()
	defer srv.Shutdown(context.Background())
	time.Sleep(100 * time.Millisecond)
	client := &http.Client{Timeout: 2 * time.Second}
	post := func(path string, v any) (map[string]any, error) {
		b, _ := json.Marshal(v)
		r, e := client.Post("http://"+addr+path, "application/json", bytes.NewReader(b))
		if e != nil {
			return nil, e
		}
		defer r.Body.Close()
		var out map[string]any
		json.NewDecoder(r.Body).Decode(&out)
		if r.StatusCode >= 300 {
			return out, fmt.Errorf("%s status %d", path, r.StatusCode)
		}
		return out, nil
	}
	x, e := post("/api/batches", map[string]any{"segmentId": "SEG-01", "waterSource": "北区水厂", "createdBy": "自检", "idempotencyKey": "self-create", "profile": map[string]any{"startMarker": "A", "endMarker": "B", "material": "球墨铸铁", "volumeM3": 10, "targetChlorineMin": 0.3, "targetChlorineMax": 1.0}})
	if e != nil {
		return e
	}
	id := x["id"].(string)
	v := int(x["version"].(float64))
	plan := domain.Plan{FlowRateM3H: 600000, DurationMin: 0.001, DisinfectantTarget: 0.6, SamplingPoints: []string{"P1", "P2"}}
	precheck, e := post("/api/batches/"+id+"/freeze", map[string]any{"action": "precheck", "expectedVersion": v, "actor": "自检", "plan": plan})
	if e != nil {
		return e
	}
	x, e = post("/api/batches/"+id+"/freeze", map[string]any{"action": "confirm", "expectedVersion": v, "actor": "自检", "idempotencyKey": "self-freeze", "confirmationSummary": precheck["confirmationSummary"], "warningsConfirmed": true, "plan": plan})
	if e != nil {
		return e
	}
	v = int(x["version"].(float64))
	frozenAt, e := time.Parse(time.RFC3339Nano, x["plan"].(map[string]any)["frozenAt"].(string))
	if e != nil {
		return e
	}
	startedAt, endedAt := frozenAt.Add(time.Millisecond), frozenAt.Add(61*time.Millisecond)
	time.Sleep(80 * time.Millisecond)
	x, e = post("/api/batches/"+id+"/rounds", map[string]any{"expectedVersion": v, "actor": "自检", "idempotencyKey": "self-round-1", "round": domain.ExecutionRound{Sequence: 1, RoundType: "flush", FlowRateM3H: 600000, StartedAt: startedAt, EndedAt: endedAt, ChlorineMgL: 0.6}})
	if e != nil {
		return e
	}
	v = int(x["version"].(float64))
	x, e = post("/api/batches/"+id+"/finish", map[string]any{"expectedVersion": v, "actor": "自检"})
	if e != nil {
		return e
	}
	v = int(x["version"].(float64))
	sampledAt := time.Now().UTC().Add(time.Second)
	x, e = post("/api/batches/"+id+"/samples", map[string]any{"expectedVersion": v, "actor": "自检", "idempotencyKey": "self-samples", "samples": []domain.WaterSample{{SamplingPoint: "P1", Witness: "见证员甲", SampledAt: sampledAt, TurbidityNTU: 0.5, ChlorineMgL: 0.6, ColonyCFUML: 10}, {SamplingPoint: "P2", Witness: "见证员乙", SampledAt: sampledAt, TurbidityNTU: 0.4, ChlorineMgL: 0.7, ColonyCFUML: 8}}})
	if e != nil {
		return e
	}
	if recorded, ok := x["recordedSamples"].([]any); !ok || len(recorded) != 2 {
		return fmt.Errorf("批量样本未完整原子登记")
	}
	if int(x["batch"].(map[string]any)["version"].(float64)) != v+1 {
		return fmt.Errorf("批量样本未在单一版本事务中登记")
	}
	v = int(x["batch"].(map[string]any)["version"].(float64))
	summaryResponse, e := client.Get("http://" + addr + "/api/batches/" + id + "/summary")
	if e != nil {
		return e
	}
	var summary map[string]any
	e = json.NewDecoder(summaryResponse.Body).Decode(&summary)
	summaryResponse.Body.Close()
	if e != nil || summaryResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("读取复核证据失败")
	}
	reviewToken := summary["reviewEvidence"].(map[string]any)["reviewToken"].(string)
	x, e = post("/api/batches/"+id+"/review", map[string]any{"expectedVersion": v, "actor": "复核", "approved": true, "reviewToken": reviewToken})
	if e != nil {
		return e
	}
	v = int(x["version"].(float64))
	x, e = post("/api/batches/"+id+"/release", map[string]any{"expectedVersion": v, "actor": "复核"})
	if e != nil {
		return e
	}
	if x["verificationDigest"] == nil {
		return fmt.Errorf("凭据核验摘要缺失")
	}
	response, e := client.Get("http://" + addr + "/api/batches/" + id + "/verify")
	if e != nil {
		return e
	}
	defer response.Body.Close()
	var verification map[string]any
	if e = json.NewDecoder(response.Body).Decode(&verification); e != nil {
		return e
	}
	if response.StatusCode != http.StatusOK || verification["passed"] != true {
		return fmt.Errorf("凭据与审计时间线核验未通过")
	}
	fmt.Println("selfcheck passed", id)
	return nil
}
