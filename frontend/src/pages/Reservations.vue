<template>
  <div class="reservations-page">
    <van-cell-group inset title="新增预约">
      <van-field v-model="form.station_id" type="number" label="机位ID" placeholder="输入机位ID" />
      <van-field :model-value="form.start_time" label="开始时间" placeholder="如 2026-08-17 10:00" @click="showStart = true" readonly />
      <van-field :model-value="form.end_time" label="结束时间" placeholder="如 2026-08-17 12:00" @click="showEnd = true" readonly />
      <van-field v-model="form.remark" label="备注" placeholder="选填" />
    </van-cell-group>
    <div class="submit-btn"><van-button round block type="primary" @click="create">提交预约</van-button></div>

    <van-dropdown-menu>
      <van-dropdown-item v-model="status" :options="statusOptions" @change="load" />
    </van-dropdown-menu>
    <van-cell-group inset title="预约列表">
      <van-cell v-for="r in list" :key="r.id" :title="`预约 #${r.id} · 机位 ${r.station_id}`" :label="`${formatTime(r.start_time)} ~ ${formatTime(r.end_time)}`">
        <template #value>
          <StatusBadge kind="reservation" :status="r.status" />
          <van-button v-if="canReschedule(r)" size="mini" type="warning" plain class="op-btn" @click="openReschedule(r)">改约</van-button>
          <van-button v-if="isStaffOrAdmin && r.status === 'confirmed'" size="mini" type="primary" class="op-btn" @click="checkIn(r)">开机</van-button>
          <van-button v-if="['pending','confirmed'].includes(r.status)" size="mini" type="danger" plain class="op-btn" @click="cancel(r)">取消</van-button>
        </template>
      </van-cell>
    </van-cell-group>
    <van-pagination v-model="page" :total-items="total" :items-per-page="pageSize" @change="load" />

    <!-- 新增预约：日期时间选择 -->
    <van-popup v-model:show="showStart" position="bottom">
      <van-date-picker v-model="startDate" title="选择开始日期" @confirm="onStartDate" @cancel="showStart = false" />
    </van-popup>
    <van-popup v-model:show="showEnd" position="bottom">
      <van-date-picker v-model="endDate" title="选择结束日期" @confirm="onEndDate" @cancel="showEnd = false" />
    </van-popup>
    <van-popup v-model:show="showStartTime" position="bottom">
      <van-time-picker v-model="startClock" title="选择开始时间" @confirm="onStartClock" @cancel="showStartTime = false" />
    </van-popup>
    <van-popup v-model:show="showEndTime" position="bottom">
      <van-time-picker v-model="endClock" title="选择结束时间" @confirm="onEndClock" @cancel="showEndTime = false" />
    </van-popup>

    <!-- 改约弹层：新机位与新时段 -->
    <van-popup v-model:show="rsPopup" position="bottom" round :style="{ maxHeight: '80%' }">
      <div class="rs-title">改约 · 预约 #{{ rsForm.id }}（开始前两小时可改）</div>
      <van-cell-group inset>
        <van-field :model-value="rsStationText" label="新机位" is-link readonly placeholder="选择新机位" @click="rsShowStation = true" />
        <van-field :model-value="rsForm.start_time" label="新开始时间" is-link readonly @click="rsShowStart = true" />
        <van-field :model-value="rsForm.end_time" label="新结束时间" is-link readonly @click="rsShowEnd = true" />
      </van-cell-group>
      <div class="submit-btn">
        <van-button round block type="primary" :loading="rsLoading" @click="submitReschedule">确认改约</van-button>
      </div>
    </van-popup>
    <van-popup v-model:show="rsShowStation" position="bottom">
      <van-picker
        :columns="stationColumns"
        :columns-field-names="{ text: 'text', value: 'id' }"
        title="选择新机位"
        @confirm="onRsStation"
        @cancel="rsShowStation = false"
      />
    </van-popup>
    <van-popup v-model:show="rsShowStart" position="bottom">
      <van-date-picker v-model="rsStartDate" title="选择新开始日期" @confirm="onRsStartDate" @cancel="rsShowStart = false" />
    </van-popup>
    <van-popup v-model:show="rsShowEnd" position="bottom">
      <van-date-picker v-model="rsEndDate" title="选择新结束日期" @confirm="onRsEndDate" @cancel="rsShowEnd = false" />
    </van-popup>
    <van-popup v-model:show="rsShowStartTime" position="bottom">
      <van-time-picker v-model="rsStartClock" title="选择新开始时间" @confirm="onRsStartClock" @cancel="rsShowStartTime = false" />
    </van-popup>
    <van-popup v-model:show="rsShowEndTime" position="bottom">
      <van-time-picker v-model="rsEndClock" title="选择新结束时间" @confirm="onRsEndClock" @cancel="rsShowEndTime = false" />
    </van-popup>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { showSuccessToast, showToast } from 'vant'
import StatusBadge from '@/components/StatusBadge.vue'
import { listReservations, createReservation, cancelReservation, rescheduleReservation, checkInReservation, type Reservation } from '@/api/reservation'
import { listAllStations, type Station } from '@/api/station'
import { STATION_STATUS_TEXT } from '@/constants'
import { formatTime } from '@/utils/format'
import { useAuth } from '@/hooks/useAuth'

const { isStaffOrAdmin, user } = useAuth()
const list = ref<Reservation[]>([])
const stations = ref<Station[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 10
const status = ref('')
const statusOptions = [
  { text: '全部状态', value: '' },
  { text: '待确认', value: 'pending' },
  { text: '已确认', value: 'confirmed' },
  { text: '已开机', value: 'checked_in' },
  { text: '已完成', value: 'completed' },
  { text: '已取消', value: 'cancelled' },
]
const form = ref({ station_id: '', start_time: '', end_time: '', remark: '' })
const startDateText = ref('')
const endDateText = ref('')
const showStart = ref(false)
const showEnd = ref(false)
const showStartTime = ref(false)
const showEndTime = ref(false)
const startDate = ref<string[]>([])
const endDate = ref<string[]>([])
const startClock = ref<string[]>(['10', '00'])
const endClock = ref<string[]>(['12', '00'])

async function load() {
  const data = await listReservations({ page: page.value, page_size: pageSize, status: status.value || undefined })
  list.value = data.list
  total.value = data.total
}

async function loadStations() {
  try {
    stations.value = await listAllStations()
  } catch {
    // 机位列表加载失败不阻塞预约列表展示
  }
}

// toISODate 将日期选择器的选中值拼成 yyyy-MM-dd。
function toISODate(values: string[]) {
  return values.map((v) => v.padStart(2, '0')).join('-')
}

// toRFC3339 将日期与时分拼成后端 time.Time 可解析的 RFC3339（东八区）。
function toRFC3339(dateText: string, clock: string[]) {
  return `${dateText}T${clock[0].padStart(2, '0')}:${clock[1].padStart(2, '0')}:00+08:00`
}

function onStartDate({ selectedValues }: any) {
  startDateText.value = toISODate(selectedValues)
  showStart.value = false
  showStartTime.value = true
}

function onStartClock({ selectedValues }: any) {
  showStartTime.value = false
  startClock.value = selectedValues
  form.value.start_time = `${startDateText.value} ${selectedValues[0]}:${selectedValues[1]}`
}

function onEndDate({ selectedValues }: any) {
  endDateText.value = toISODate(selectedValues)
  showEnd.value = false
  showEndTime.value = true
}

function onEndClock({ selectedValues }: any) {
  showEndTime.value = false
  endClock.value = selectedValues
  form.value.end_time = `${endDateText.value} ${selectedValues[0]}:${selectedValues[1]}`
}

async function create() {
  const stationId = Number(form.value.station_id)
  if (!stationId || !form.value.start_time || !form.value.end_time) {
    showToast('请填写机位ID与起止时间')
    return
  }
  const [sd, st] = form.value.start_time.split(' ')
  const [ed, et] = form.value.end_time.split(' ')
  await createReservation({
    station_id: stationId,
    start_time: toRFC3339(sd, st.split(':')),
    end_time: toRFC3339(ed, et.split(':')),
    remark: form.value.remark,
  })
  showSuccessToast('预约成功')
  form.value = { station_id: '', start_time: '', end_time: '', remark: '' }
  load()
}

async function cancel(r: Reservation) {
  await cancelReservation(r.id)
  showSuccessToast('已取消')
  load()
}

async function checkIn(r: Reservation) {
  await checkInReservation(r.id)
  showSuccessToast('开机成功')
  load()
}

// 仅本人、待确认/已确认且距开始超过两小时的预约可改约（后端同样强校验）。
function canReschedule(r: Reservation) {
  if (!user.value || r.user_id !== user.value.id) return false
  if (!['pending', 'confirmed'].includes(r.status)) return false
  return new Date(r.start_time).getTime() - Date.now() > 2 * 60 * 60 * 1000
}

// ---- 改约 ----
const rsPopup = ref(false)
const rsLoading = ref(false)
const rsForm = ref<{ id: number; station_id: number; start_time: string; end_time: string }>({
  id: 0,
  station_id: 0,
  start_time: '',
  end_time: '',
})
const rsShowStation = ref(false)
const rsShowStart = ref(false)
const rsShowEnd = ref(false)
const rsShowStartTime = ref(false)
const rsShowEndTime = ref(false)
const rsStartDate = ref<string[]>([])
const rsEndDate = ref<string[]>([])
const rsStartClock = ref<string[]>(['10', '00'])
const rsEndClock = ref<string[]>(['12', '00'])
const rsStartDateText = ref('')
const rsEndDateText = ref('')

const stationColumns = ref<{ id: number; text: string }[]>([])

const rsStationText = ref('')

function splitDateTime(v: string): { date: string; clock: string[] } {
  const text = formatTime(v) // yyyy-MM-dd HH:mm:ss
  return { date: text.slice(0, 10), clock: [text.slice(11, 13), text.slice(14, 16)] }
}

function openReschedule(r: Reservation) {
  const start = splitDateTime(r.start_time)
  const end = splitDateTime(r.end_time)
  rsForm.value = { id: r.id, station_id: r.station_id, start_time: `${start.date} ${start.clock[0]}:${start.clock[1]}`, end_time: `${end.date} ${end.clock[0]}:${end.clock[1]}` }
  rsStartDateText.value = start.date
  rsEndDateText.value = end.date
  rsStartDate.value = start.date.split('-')
  rsEndDate.value = end.date.split('-')
  rsStartClock.value = start.clock
  rsEndClock.value = end.clock
  syncStationText(r.station_id)
  stationColumns.value = stations.value.map((s) => ({ id: s.id, text: `机位 ${s.id} · ${s.name}（${s.area}，${STATION_STATUS_TEXT[s.status] || s.status}）` }))
  rsPopup.value = true
}

function syncStationText(stationId: number) {
  const s = stations.value.find((x) => x.id === stationId)
  rsStationText.value = s ? `机位 ${s.id} · ${s.name}（${s.area}，${STATION_STATUS_TEXT[s.status] || s.status}）` : `机位 ${stationId}`
}

function onRsStation({ selectedOptions }: any) {
  const picked = selectedOptions[0]
  rsForm.value.station_id = picked.id
  syncStationText(picked.id)
  rsShowStation.value = false
}

function onRsStartDate({ selectedValues }: any) {
  rsStartDateText.value = toISODate(selectedValues)
  rsShowStart.value = false
  rsShowStartTime.value = true
}

function onRsStartClock({ selectedValues }: any) {
  rsStartClock.value = selectedValues
  rsShowStartTime.value = false
  rsForm.value.start_time = `${rsStartDateText.value} ${selectedValues[0]}:${selectedValues[1]}`
}

function onRsEndDate({ selectedValues }: any) {
  rsEndDateText.value = toISODate(selectedValues)
  rsShowEnd.value = false
  rsShowEndTime.value = true
}

function onRsEndClock({ selectedValues }: any) {
  rsEndClock.value = selectedValues
  rsShowEndTime.value = false
  rsForm.value.end_time = `${rsEndDateText.value} ${selectedValues[0]}:${selectedValues[1]}`
}

async function submitReschedule() {
  if (!rsForm.value.station_id) {
    showToast('请选择新机位')
    return
  }
  if (!rsForm.value.start_time || !rsForm.value.end_time) {
    showToast('请选择新的起止时间')
    return
  }
  const [sd, st] = rsForm.value.start_time.split(' ')
  const [ed, et] = rsForm.value.end_time.split(' ')
  rsLoading.value = true
  try {
    await rescheduleReservation(rsForm.value.id, {
      station_id: rsForm.value.station_id,
      start_time: toRFC3339(sd, st.split(':')),
      end_time: toRFC3339(ed, et.split(':')),
    })
    showSuccessToast('改约成功')
    rsPopup.value = false
    load()
  } finally {
    rsLoading.value = false
  }
}

onMounted(() => {
  load()
  loadStations()
})
</script>

<style scoped>
.submit-btn { margin: 12px 16px; }
.op-btn { margin-left: 6px; }
.rs-title { padding: 16px; font-weight: 600; text-align: center; }
</style>
