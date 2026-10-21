package server

import (
	"net/http"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

// requireCatalogParam 读取必填查询参数.
func requireCatalogParam(r *http.Request, name string) (string, error) {
	raw, ok := queryValue(r, name)
	if !ok || raw == "" {
		return "", invalidRequestf("缺少 %s 参数", name)
	}
	return raw, nil
}

// requireCatalog 检查目录接口的前置条件: 可用会话与目录数据源.
func (s *Server) requireCatalog(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := s.requireSession(w, r); !ok {
		return false
	}
	if s.catalog == nil {
		s.writeProblem(w, r, http.StatusServiceUnavailable, protocol.CodeUnavailable, "", "ECNU 目录数据源尚未配置")
		return false
	}
	return true
}

// handleDistricts 返回地区与片区列表.
func (s *Server) handleDistricts(w http.ResponseWriter, r *http.Request) {
	if !s.requireCatalog(w, r) {
		return
	}
	data, err := s.catalog.Districts(r.Context())
	if err != nil {
		s.writeUpstreamProblem(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, data)
}

// handleBuildings 返回建筑列表.
func (s *Server) handleBuildings(w http.ResponseWriter, r *http.Request) {
	areaID, err := requireCatalogParam(r, "area_id")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	districtID, err := requireCatalogParam(r, "district_id")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	if !s.requireCatalog(w, r) {
		return
	}
	data, err := s.catalog.Buildings(r.Context(), areaID, districtID)
	if err != nil {
		s.writeUpstreamProblem(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, data)
}

// handleFloors 返回楼层列表.
func (s *Server) handleFloors(w http.ResponseWriter, r *http.Request) {
	areaID, err := requireCatalogParam(r, "area_id")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	districtID, err := requireCatalogParam(r, "district_id")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	buildingID, err := requireCatalogParam(r, "building_id")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	if !s.requireCatalog(w, r) {
		return
	}
	data, err := s.catalog.Floors(r.Context(), areaID, districtID, buildingID)
	if err != nil {
		s.writeUpstreamProblem(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, data)
}

// handleCatalogRooms 返回房间列表.
func (s *Server) handleCatalogRooms(w http.ResponseWriter, r *http.Request) {
	areaID, err := requireCatalogParam(r, "area_id")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	districtID, err := requireCatalogParam(r, "district_id")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	buildingID, err := requireCatalogParam(r, "building_id")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	floorID, err := requireCatalogParam(r, "floor_id")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	if !s.requireCatalog(w, r) {
		return
	}
	data, err := s.catalog.Rooms(r.Context(), areaID, districtID, buildingID, floorID)
	if err != nil {
		s.writeUpstreamProblem(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, data)
}
