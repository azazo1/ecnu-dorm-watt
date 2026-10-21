<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type {
  AreaOption,
  BuildingOption,
  CatalogRoom,
  DistrictOption,
  FloorOption,
} from '@/api/types'
import { toUiError, type UiError } from '@/utils/errors'
import AppIcon from './AppIcon.vue'
import ErrorState from './ErrorState.vue'
import type { RoomSelection } from './room-selection'

const props = withDefaults(
  defineProps<{
    disabled?: boolean
    busy?: boolean
  }>(),
  {
    disabled: false,
    busy: false,
  },
)

const emit = defineEmits<{ select: [selection: RoomSelection] }>()

const areas = ref<AreaOption[]>([])
const districts = ref<DistrictOption[]>([])
const buildings = ref<BuildingOption[]>([])
const floors = ref<FloorOption[]>([])
const rooms = ref<CatalogRoom[]>([])

const areaId = ref('')
const districtId = ref('')
const buildingId = ref('')
const floorId = ref('')
const roomId = ref('')

const catalogLoading = ref(false)
const levelLoading = ref('')
const loaded = ref(false)
const error = ref<UiError | null>(null)
const emptyMessage = ref('')

let retryAction: (() => Promise<void>) | null = null

const area = computed(() => areas.value.find((item) => item.area_id === areaId.value) ?? null)
const district = computed(
  () => districts.value.find((item) => item.district_id === districtId.value) ?? null,
)
const building = computed(
  () => buildings.value.find((item) => item.building_id === buildingId.value) ?? null,
)
const floor = computed(() => floors.value.find((item) => item.floor_id === floorId.value) ?? null)
const room = computed(() => rooms.value.find((item) => item.room_id === roomId.value) ?? null)

const pathLabel = computed(() => {
  const parts = [
    area.value?.area_name,
    district.value?.district_name,
    building.value?.building_name,
    floor.value?.floor_name ? `${floor.value.floor_name} 层` : null,
    room.value?.room_name,
  ].filter(Boolean)
  return parts.join(' / ')
})

const canUse = computed(
  () => Boolean(room.value && area.value && district.value && building.value && floor.value) &&
    !props.busy &&
    !props.disabled,
)

function clearLevel(from: 'building' | 'floor' | 'room'): void {
  if (from === 'building') {
    buildings.value = []
    buildingId.value = ''
  }
  if (from === 'building' || from === 'floor') {
    floors.value = []
    floorId.value = ''
  }
  rooms.value = []
  roomId.value = ''
}

function resetError(): void {
  error.value = null
  emptyMessage.value = ''
}

async function loadDistricts(): Promise<void> {
  retryAction = loadDistricts
  catalogLoading.value = true
  loaded.value = false
  resetError()
  try {
    const data = await api.getDistricts()
    areas.value = data.areas
    districts.value = data.districts
    loaded.value = true
    if (areas.value.length === 1) {
      areaId.value = areas.value[0].area_id
    }
    if (districts.value.length === 1) {
      districtId.value = districts.value[0].district_id
    }
    if (areaId.value && districtId.value) {
      await loadBuildings()
    }
  } catch (caught) {
    error.value = toUiError(caught)
  } finally {
    catalogLoading.value = false
  }
}

async function loadBuildings(): Promise<void> {
  if (!areaId.value || !districtId.value) {
    return
  }
  retryAction = loadBuildings
  levelLoading.value = 'buildings'
  resetError()
  try {
    const data = await api.getBuildings({
      area_id: areaId.value,
      district_id: districtId.value,
    })
    buildings.value = data.buildings
    buildingId.value = ''
    clearLevel('floor')
    if (buildings.value.length === 0) {
      emptyMessage.value = '该地区与公寓区下没有建筑.'
    }
  } catch (caught) {
    error.value = toUiError(caught)
  } finally {
    levelLoading.value = ''
  }
}

async function loadFloors(): Promise<void> {
  if (!areaId.value || !districtId.value || !buildingId.value) {
    return
  }
  retryAction = loadFloors
  levelLoading.value = 'floors'
  resetError()
  try {
    const data = await api.getFloors({
      area_id: areaId.value,
      district_id: districtId.value,
      building_id: buildingId.value,
    })
    floors.value = data.floors
    floorId.value = ''
    clearLevel('room')
    if (floors.value.length === 0) {
      emptyMessage.value = '该建筑下没有楼层数据.'
    }
  } catch (caught) {
    error.value = toUiError(caught)
  } finally {
    levelLoading.value = ''
  }
}

async function loadRooms(): Promise<void> {
  if (!areaId.value || !districtId.value || !buildingId.value || !floorId.value) {
    return
  }
  retryAction = loadRooms
  levelLoading.value = 'rooms'
  resetError()
  try {
    const data = await api.getCatalogRooms({
      area_id: areaId.value,
      district_id: districtId.value,
      building_id: buildingId.value,
      floor_id: floorId.value,
    })
    rooms.value = data.rooms
    roomId.value = ''
    if (rooms.value.length === 0) {
      emptyMessage.value = '该楼层下没有房间数据.'
    }
  } catch (caught) {
    error.value = toUiError(caught)
  } finally {
    levelLoading.value = ''
  }
}

function onAreaChange(): void {
  districtId.value = ''
  clearLevel('building')
  resetError()
  if (districts.value.length === 1) {
    districtId.value = districts.value[0].district_id
  }
  if (areaId.value && districtId.value) {
    void loadBuildings()
  }
}

function onDistrictChange(): void {
  clearLevel('building')
  resetError()
  void loadBuildings()
}

function onBuildingChange(): void {
  clearLevel('floor')
  resetError()
  void loadFloors()
}

function onFloorChange(): void {
  clearLevel('room')
  resetError()
  void loadRooms()
}

function retry(): void {
  void retryAction?.()
}

function choose(): void {
  if (!canUse.value || !room.value || !area.value || !district.value || !building.value || !floor.value) {
    return
  }
  emit('select', {
    room_no: room.value.room_id,
    room_name: room.value.room_name,
    elcarea: Number(area.value.area_id),
    elcbuis: building.value.building_id,
    area_name: area.value.area_name,
    district_name: district.value.district_name,
    building_name: building.value.building_name,
    floor_name: floor.value.floor_name,
  })
}

onMounted(() => {
  void loadDistricts()
})
</script>

<template>
  <div class="room-cascader" :aria-busy="catalogLoading || Boolean(levelLoading)">
    <div v-if="catalogLoading" class="catalog-state">
      <span class="spinner" aria-hidden="true" />
      正在加载地区与公寓区...
    </div>
    <ErrorState v-else-if="error" :error="error" compact @retry="retry" />
    <template v-else>
      <div class="form-grid">
        <div class="field">
          <label for="room-area">地区</label>
          <select
            id="room-area"
            v-model="areaId"
            class="select"
            :disabled="disabled || busy || areas.length === 0"
            @change="onAreaChange"
          >
            <option value="">请选择地区</option>
            <option v-for="item in areas" :key="item.area_id" :value="item.area_id">
              {{ item.area_name }}
            </option>
          </select>
        </div>

        <div class="field">
          <label for="room-district">公寓区</label>
          <select
            id="room-district"
            v-model="districtId"
            class="select"
            :disabled="disabled || busy || !areaId || districts.length === 0"
            @change="onDistrictChange"
          >
            <option value="">请选择公寓区</option>
            <option v-for="item in districts" :key="item.district_id" :value="item.district_id">
              {{ item.district_name }}
            </option>
          </select>
        </div>

        <div class="field">
          <label for="room-building">建筑</label>
          <select
            id="room-building"
            v-model="buildingId"
            class="select"
            :disabled="disabled || busy || !districtId || buildings.length === 0"
            @change="onBuildingChange"
          >
            <option value="">请选择建筑</option>
            <option v-for="item in buildings" :key="item.building_id" :value="item.building_id">
              {{ item.building_name }}
            </option>
          </select>
        </div>

        <div class="field">
          <label for="room-floor">楼层</label>
          <select
            id="room-floor"
            v-model="floorId"
            class="select"
            :disabled="disabled || busy || !buildingId || floors.length === 0"
            @change="onFloorChange"
          >
            <option value="">请选择楼层</option>
            <option v-for="item in floors" :key="item.floor_id" :value="item.floor_id">
              {{ item.floor_name }} 层
            </option>
          </select>
        </div>

        <div class="field field-wide">
          <label for="room-number">房间</label>
          <select
            id="room-number"
            v-model="roomId"
            class="select"
            :disabled="disabled || busy || !floorId || rooms.length === 0"
          >
            <option value="">请选择房间</option>
            <option v-for="item in rooms" :key="item.room_id" :value="item.room_id">
              {{ item.room_name }}
            </option>
          </select>
        </div>
      </div>

      <div v-if="levelLoading" class="catalog-state">
        <span class="spinner" aria-hidden="true" />
        正在加载{{ levelLoading === 'buildings' ? '建筑' : levelLoading === 'floors' ? '楼层' : '房间' }}...
      </div>

      <p v-if="emptyMessage" class="catalog-empty">{{ emptyMessage }}</p>
      <p v-else-if="loaded && districts.length === 0" class="catalog-empty">
        学校目录没有返回公寓区, 请确认 ECNU 登录状态.
      </p>

      <div v-if="pathLabel" class="catalog-path">
        <AppIcon name="room" :size="16" />
        <span>{{ pathLabel }}</span>
      </div>

      <div class="catalog-actions">
        <button
          type="button"
          class="btn btn-primary"
          :disabled="!canUse"
          @click="choose"
        >
          {{ busy ? '正在切换...' : '使用此房间' }}
        </button>
        <span class="field-hint">选择后会把该房间设为活动采集房间.</span>
      </div>
    </template>
  </div>
</template>

<style scoped>
.room-cascader {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.field-wide {
  grid-column: 1 / -1;
}

.catalog-state {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-muted);
  font-size: 13px;
}

.spinner {
  width: 14px;
  height: 14px;
  border: 2px solid var(--border);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

.catalog-empty {
  color: var(--text-muted);
  font-size: 13px;
}

.catalog-path {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  background: var(--surface-soft);
  color: var(--text-muted);
  font-size: 12px;
}

.catalog-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

@media (max-width: 640px) {
  .field-wide {
    grid-column: auto;
  }
}
</style>
