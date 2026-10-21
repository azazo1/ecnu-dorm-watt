package ecnu_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/ecnu"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *ecnu.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := ecnu.New(ecnu.Options{
		BaseURL:    srv.URL,
		Timeout:    5 * time.Second,
		UserAgent:  "test-agent",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	return client
}

func testRoom() ecnu.Room {
	return ecnu.Room{RoomNo: "4408_MH_83_257", Elcarea: 102, Elcbuis: "new-83_MH"}
}

func testSession() ecnu.Session {
	return ecnu.Session{JSESSIONID: "jsession", Cookie: "cookie-value", CSRFToken: "csrf-token"}
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	_, _ = w.Write([]byte(body))
}

func TestQueryDegreeSuccess(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("方法错误: %s", r.Method)
		}
		if r.URL.Path != "/epaycas/electric/queryelectricbill" {
			t.Errorf("路径错误: %s", r.URL.Path)
		}
		if got := r.PostFormValue("sysid"); got != "1" {
			t.Errorf("sysid 错误: %s", got)
		}
		if got := r.PostFormValue("roomNo"); got != "4408_MH_83_257" {
			t.Errorf("roomNo 错误: %s", got)
		}
		if got := r.PostFormValue("elcarea"); got != "102" {
			t.Errorf("elcarea 错误: %s", got)
		}
		if got := r.PostFormValue("elcbuis"); got != "new-83_MH" {
			t.Errorf("elcbuis 错误: %s", got)
		}
		if got := r.Header.Get("Cookie"); got != "JSESSIONID=jsession; cookie=cookie-value" {
			t.Errorf("Cookie 错误: %s", got)
		}
		if got := r.Header.Get("X-CSRF-TOKEN"); got != "csrf-token" {
			t.Errorf("CSRF 错误: %s", got)
		}
		if got := r.Header.Get("User-Agent"); got != "test-agent" {
			t.Errorf("UA 错误: %s", got)
		}
		writeJSON(w, `{"retcode":0,"retmsg":"成功","restElecDegree":33.43}`)
	})

	degree, err := client.QueryDegree(context.Background(), testRoom(), testSession())
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if degree != 33.43 {
		t.Fatalf("度数错误: %v", degree)
	}
}

func TestQueryDegreeBusinessError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, `{"retcode":7,"retmsg":"系统繁忙","restElecDegree":1}`)
	})
	_, err := client.QueryDegree(context.Background(), testRoom(), testSession())
	if !errors.Is(err, ecnu.ErrUpstream) {
		t.Fatalf("期望 ErrUpstream, 实际: %v", err)
	}
	if !strings.Contains(err.Error(), "系统繁忙") {
		t.Fatalf("错误文案缺少 retmsg: %v", err)
	}
}

func TestQueryDegreeNotLoggedIn(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>login</body></html>"))
	})
	_, err := client.QueryDegree(context.Background(), testRoom(), testSession())
	if !errors.Is(err, ecnu.ErrNotLoggedIn) {
		t.Fatalf("期望 ErrNotLoggedIn, 实际: %v", err)
	}
}

func TestQueryDegreeRedirectToSSO(t *testing.T) {
	var hits int
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Location", "https://sso.ecnu.edu.cn/acitauth")
		w.WriteHeader(http.StatusFound)
	})
	_, err := client.QueryDegree(context.Background(), testRoom(), testSession())
	if !errors.Is(err, ecnu.ErrNotLoggedIn) {
		t.Fatalf("期望 ErrNotLoggedIn, 实际: %v", err)
	}
	if hits != 1 {
		t.Fatalf("不应跟随重定向, 实际请求 %d 次", hits)
	}
}

func TestQueryDegreeMissingDegree(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, `{"retcode":0,"retmsg":"成功"}`)
	})
	_, err := client.QueryDegree(context.Background(), testRoom(), testSession())
	if !errors.Is(err, ecnu.ErrNoDegree) {
		t.Fatalf("期望 ErrNoDegree, 实际: %v", err)
	}
}

func TestQueryDegreeServerError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err := client.QueryDegree(context.Background(), testRoom(), testSession())
	if !errors.Is(err, ecnu.ErrUpstream) {
		t.Fatalf("期望 ErrUpstream, 实际: %v", err)
	}
}

func TestQueryDegreeBodyTooLarge(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"retcode":0,"retmsg":"成功","restElecDegree":1,"pad":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", ecnu.MaxResponseBytes)))
		_, _ = w.Write([]byte(`"}`))
	})
	_, err := client.QueryDegree(context.Background(), testRoom(), testSession())
	if !errors.Is(err, ecnu.ErrUpstream) {
		t.Fatalf("期望 ErrUpstream, 实际: %v", err)
	}
}

func TestQueryDegreeRequiresSession(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("缺少凭据时不应发起请求")
	})
	_, err := client.QueryDegree(context.Background(), testRoom(), ecnu.Session{})
	if !errors.Is(err, ecnu.ErrNotLoggedIn) {
		t.Fatalf("期望 ErrNotLoggedIn, 实际: %v", err)
	}
}

func TestCatalogMapping(t *testing.T) {
	districtsBody := `{
		"retcode": 0, "retmsg": "成功",
		"areas": [{"areaId":"102","areaName":"华东师范大学"}],
		"districts": [{"districtId":"MH","districtName":"剑川路公寓"}],
		"buils": [{"buiId":"new-83_MH","buiName":"闵行本科生4号楼"}],
		"floors": [{"floorId":"257","floorName":"4"}],
		"rooms": [{"roomId":"4408_MH_83_257","roomName":"4408"}]
	}`

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("解析表单失败: %v", err)
		}
		if got := r.PostForm.Get("sysid"); got != "1" {
			t.Errorf("sysid 错误: %s", got)
		}
		switch r.URL.Path {
		case "/epaycas/electric/queryelectricarea":
			writeJSON(w, districtsBody)
		case "/epaycas/electric/queryelectricbuis":
			if r.PostForm.Get("area") != "102" || r.PostForm.Get("district") != "MH" {
				t.Errorf("建筑表单错误: %v", r.PostForm)
			}
			writeJSON(w, `{"retcode":0,"retmsg":"成功","buils":[{"buiId":"new-83_MH","buiName":"闵行本科生4号楼"}]}`)
		case "/epaycas/electric/queryelectricfloors":
			if r.PostForm.Get("build") != "new-83_MH" {
				t.Errorf("楼层表单错误: %v", r.PostForm)
			}
			writeJSON(w, `{"retcode":0,"retmsg":"成功","floors":[{"floorId":"257","floorName":"4"}]}`)
		case "/epaycas/electric/queryelectricrooms":
			if r.PostForm.Get("floor") != "257" {
				t.Errorf("房间表单错误: %v", r.PostForm)
			}
			writeJSON(w, `{"retcode":0,"retmsg":"成功","rooms":[{"roomId":"4408_MH_83_257","roomName":"4408"}]}`)
		default:
			t.Errorf("未知路径: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	ctx := context.Background()
	session := testSession()

	districts, err := client.Districts(ctx, session)
	if err != nil {
		t.Fatalf("查询地区失败: %v", err)
	}
	wantDistricts := protocol.Districts{
		Areas:     []protocol.Area{{AreaID: "102", AreaName: "华东师范大学"}},
		Districts: []protocol.District{{DistrictID: "MH", DistrictName: "剑川路公寓"}},
	}
	if !reflect.DeepEqual(districts, wantDistricts) {
		t.Fatalf("地区映射错误: %+v", districts)
	}

	buildings, err := client.Buildings(ctx, session, "102", "MH")
	if err != nil {
		t.Fatalf("查询建筑失败: %v", err)
	}
	wantBuildings := []protocol.Building{{BuildingID: "new-83_MH", BuildingName: "闵行本科生4号楼"}}
	if !reflect.DeepEqual(buildings.Buildings, wantBuildings) {
		t.Fatalf("建筑映射错误: %+v", buildings)
	}

	floors, err := client.Floors(ctx, session, "102", "MH", "new-83_MH")
	if err != nil {
		t.Fatalf("查询楼层失败: %v", err)
	}
	wantFloors := []protocol.Floor{{FloorID: "257", FloorName: "4"}}
	if !reflect.DeepEqual(floors.Floors, wantFloors) {
		t.Fatalf("楼层映射错误: %+v", floors)
	}

	rooms, err := client.Rooms(ctx, session, "102", "MH", "new-83_MH", "257")
	if err != nil {
		t.Fatalf("查询房间失败: %v", err)
	}
	wantRooms := []protocol.CatalogRoom{{RoomID: "4408_MH_83_257", RoomName: "4408"}}
	if !reflect.DeepEqual(rooms.Rooms, wantRooms) {
		t.Fatalf("房间映射错误: %+v", rooms)
	}
}

func TestCatalogBusinessError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, `{"retcode":3,"retmsg":"无权限"}`)
	})
	_, err := client.Districts(context.Background(), testSession())
	if !errors.Is(err, ecnu.ErrUpstream) {
		t.Fatalf("期望 ErrUpstream, 实际: %v", err)
	}
	if !strings.Contains(err.Error(), "无权限") {
		t.Fatalf("错误文案缺少 retmsg: %v", err)
	}
}

func roomInfoHandler(t *testing.T, floorID string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/epaycas/electric/queryelectricarea":
			writeJSON(w, `{"retcode":0,"retmsg":"成功",
				"areas":[{"areaId":"102","areaName":"华东师范大学"}],
				"districts":[{"districtId":"MH","districtName":"剑川路公寓"}]}`)
		case "/epaycas/electric/queryelectricbuis":
			writeJSON(w, `{"retcode":0,"retmsg":"成功",
				"buils":[{"buiId":"new-83_MH","buiName":"闵行本科生4号楼"}]}`)
		case "/epaycas/electric/queryelectricfloors":
			writeJSON(w, `{"retcode":0,"retmsg":"成功","floors":[{"floorId":"`+floorID+`","floorName":"4"}]}`)
		case "/epaycas/electric/queryelectricrooms":
			writeJSON(w, `{"retcode":0,"retmsg":"成功",
				"rooms":[{"roomId":"4408_MH_83_257","roomName":"4408"}]}`)
		default:
			t.Errorf("未知路径: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestRoomInfoSuccess(t *testing.T) {
	client := newTestClient(t, roomInfoHandler(t, "257"))
	info, err := client.RoomInfo(context.Background(), testRoom(), testSession())
	if err != nil {
		t.Fatalf("解析房间信息失败: %v", err)
	}
	if info.Area.AreaID != "102" {
		t.Errorf("地区错误: %+v", info.Area)
	}
	if info.District.DistrictID != "MH" {
		t.Errorf("行政区错误: %+v", info.District)
	}
	if info.Building.BuildingID != "new-83_MH" || info.Building.BuildingName != "闵行本科生4号楼" {
		t.Errorf("建筑错误: %+v", info.Building)
	}
	if info.Floor.FloorID != "257" {
		t.Errorf("楼层错误: %+v", info.Floor)
	}
	if info.Room.RoomID != "4408_MH_83_257" || info.Room.RoomName != "4408" {
		t.Errorf("房间错误: %+v", info.Room)
	}
}

func TestRoomInfoIncompleteCatalog(t *testing.T) {
	client := newTestClient(t, roomInfoHandler(t, "999"))
	_, err := client.RoomInfo(context.Background(), testRoom(), testSession())
	if !errors.Is(err, ecnu.ErrCatalogIncomplete) {
		t.Fatalf("期望 ErrCatalogIncomplete, 实际: %v", err)
	}
}

func TestRoomInfoInvalidRoomNo(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("房间号非法时不应发起请求")
	})
	_, err := client.RoomInfo(context.Background(), ecnu.Room{RoomNo: "4408_MH", Elcarea: 102, Elcbuis: "new-83_MH"}, testSession())
	if err == nil {
		t.Fatal("期望房间号解析失败")
	}
	if errors.Is(err, ecnu.ErrCatalogIncomplete) || errors.Is(err, ecnu.ErrUpstream) {
		t.Fatalf("不应归类为上游错误: %v", err)
	}
}

func TestNewRejectsInvalidBaseURL(t *testing.T) {
	if _, err := ecnu.New(ecnu.Options{BaseURL: "ftp://example.com"}); err == nil {
		t.Fatal("期望非法协议被拒绝")
	}
	if _, err := ecnu.New(ecnu.Options{BaseURL: "https://"}); err == nil {
		t.Fatal("期望缺少主机名被拒绝")
	}
}

func TestErrorCodesSurviveWrapping(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{ecnu.ErrNotLoggedIn, protocol.CodeECNUNotLoggedIn},
		{ecnu.ErrUpstream, protocol.CodeECNUUpstream},
		{ecnu.ErrNoDegree, protocol.CodeECNUNoDegree},
		{ecnu.ErrCatalogIncomplete, protocol.CodeCatalogIncomplete},
	}
	for _, tc := range cases {
		var coded interface{ Code() string }
		wrapped := fmt.Errorf("上游调用失败: %w", tc.err)
		if !errors.As(wrapped, &coded) {
			t.Fatalf("%v 未实现 Code()", tc.err)
		}
		if coded.Code() != tc.code {
			t.Fatalf("错误码错误: %s, 期望 %s", coded.Code(), tc.code)
		}
		if !errors.Is(wrapped, tc.err) {
			t.Fatalf("包装后 errors.Is 失败: %v", tc.err)
		}
	}
}
