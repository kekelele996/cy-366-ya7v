<template>
  <div class="reservations-page">
    <van-cell-group inset title="新增预约">
      <van-field v-model="form.station_id" type="number" label="机位ID" placeholder="输入机位ID" />
      <van-field :model-value="form.start_time" label="开始时间" placeholder="如 2026-08-17 10:00:00" @click="showStart = true" readonly />
      <van-field :model-value="form.end_time" label="结束时间" placeholder="如 2026-08-17 12:00:00" @click="showEnd = true" readonly />
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
          <van-button v-if="isStaffOrAdmin && r.status === 'confirmed'" size="mini" type="primary" class="op-btn" @click="checkIn(r)">开机</van-button>
          <van-button v-if="canReschedule(r)" size="mini" type="warning" plain class="op-btn" @click="openReschedule(r)">改约</van-button>
          <van-button v-if="['pending','confirmed'].includes(r.status)" size="mini" type="danger" plain class="op-btn" @click="cancel(r)">取消</van-button>
        </template>
      </van-cell>
    </van-cell-group>
    <van-pagination v-model="page" :total-items="total" :items-per-page="pageSize" @change="load" />

    <van-popup v-model:show="showReschedule" position="bottom" round>
      <van-cell-group inset :title="`改约 #${rescheduleTarget?.id ?? ''}`">
        <van-cell v-if="rescheduleTarget" title="当前机位/时段" :label="`机位 ${rescheduleTarget.station_id} · ${formatTime(rescheduleTarget.start_time)} ~ ${formatTime(rescheduleTarget.end_time)}`" />
        <van-field v-model="rescheduleForm.station_id" type="number" label="新机位ID" placeholder="输入新机位ID" />
        <van-field :model-value="rescheduleForm.start_time" label="新开始时间" placeholder="选择开始日期" readonly @click="showRsStart = true" />
        <van-field :model-value="rescheduleForm.end_time" label="新结束时间" placeholder="选择结束日期" readonly @click="showRsEnd = true" />
      </van-cell-group>
      <div class="submit-btn"><van-button round block type="primary" @click="submitReschedule">确认改约</van-button></div>
    </van-popup>
    <van-popup v-model:show="showRsStart" position="bottom">
      <van-date-picker v-model="rsStartDate" title="选择开始日期" @confirm="onRsStartDate" @cancel="showRsStart = false" />
    </van-popup>
    <van-popup v-model:show="showRsEnd" position="bottom">
      <van-date-picker v-model="rsEndDate" title="选择结束日期" @confirm="onRsEndDate" @cancel="showRsEnd = false" />
    </van-popup>

    <van-popup v-model:show="showStart" position="bottom">
      <van-date-picker v-model="startDate" title="选择开始日期" @confirm="onStartDate" @cancel="showStart = false" />
    </van-popup>
    <van-popup v-model:show="showEnd" position="bottom">
      <van-date-picker v-model="endDate" title="选择结束日期" @confirm="onEndDate" @cancel="showEnd = false" />
    </van-popup>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { showSuccessToast, showToast } from 'vant'
import StatusBadge from '@/components/StatusBadge.vue'
import { listReservations, createReservation, cancelReservation, checkInReservation, rescheduleReservation, type Reservation } from '@/api/reservation'
import { formatTime } from '@/utils/format'
import { useAuth } from '@/hooks/useAuth'

const { isStaffOrAdmin, user } = useAuth()
const list = ref<Reservation[]>([])
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
const showStart = ref(false)
const showEnd = ref(false)
const startDate = ref<Date[]>([])
const endDate = ref<Date[]>([])
const showReschedule = ref(false)
const rescheduleTarget = ref<Reservation | null>(null)
const rescheduleForm = ref({ station_id: '', start_time: '', end_time: '' })
const showRsStart = ref(false)
const showRsEnd = ref(false)
const rsStartDate = ref<Date[]>([])
const rsEndDate = ref<Date[]>([])

async function load() {
  const data = await listReservations({ page: page.value, page_size: pageSize, status: status.value || undefined })
  list.value = data.list
  total.value = data.total
}

function onStartDate({ selectedValues }: any) {
  form.value.start_time = `${selectedValues.join('-')} 10:00:00`
  showStart.value = false
}

function onEndDate({ selectedValues }: any) {
  form.value.end_time = `${selectedValues.join('-')} 12:00:00`
  showEnd.value = false
}

async function create() {
  const stationId = Number(form.value.station_id)
  if (!stationId || !form.value.start_time || !form.value.end_time) {
    showToast('请填写机位ID与起止时间')
    return
  }
  await createReservation({ station_id: stationId, start_time: form.value.start_time, end_time: form.value.end_time, remark: form.value.remark })
  showSuccessToast('预约成功')
  form.value = { station_id: '', start_time: '', end_time: '', remark: '' }
  load()
}

async function cancel(r: Reservation) {
  await cancelReservation(r.id)
  showSuccessToast('已取消')
  load()
}

// 本人待确认/已确认且距开始超过 2 小时的预约可改约（店员/管理员不受本人限制）。
function canReschedule(r: Reservation) {
  if (!['pending', 'confirmed'].includes(r.status)) return false
  if (!isStaffOrAdmin.value && r.user_id !== user.value?.id) return false
  return new Date(r.start_time).getTime() - Date.now() > 2 * 3600 * 1000
}

function openReschedule(r: Reservation) {
  rescheduleTarget.value = r
  rescheduleForm.value = { station_id: String(r.station_id), start_time: '', end_time: '' }
  showReschedule.value = true
}

function onRsStartDate({ selectedValues }: any) {
  rescheduleForm.value.start_time = `${selectedValues.join('-')} 10:00:00`
  showRsStart.value = false
}

function onRsEndDate({ selectedValues }: any) {
  rescheduleForm.value.end_time = `${selectedValues.join('-')} 12:00:00`
  showRsEnd.value = false
}

// 后端按 RFC3339 解析时间，本地时间补上时区偏移。
function toRFC3339(local: string): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  const offset = -new Date().getTimezoneOffset()
  const sign = offset >= 0 ? '+' : '-'
  return `${local.replace(' ', 'T')}${sign}${pad(Math.floor(Math.abs(offset) / 60))}:${pad(Math.abs(offset) % 60)}`
}

async function submitReschedule() {
  const target = rescheduleTarget.value
  if (!target) return
  const stationId = Number(rescheduleForm.value.station_id)
  if (!stationId || !rescheduleForm.value.start_time || !rescheduleForm.value.end_time) {
    showToast('请填写新机位ID与起止时间')
    return
  }
  await rescheduleReservation(target.id, {
    station_id: stationId,
    start_time: toRFC3339(rescheduleForm.value.start_time),
    end_time: toRFC3339(rescheduleForm.value.end_time),
  })
  showSuccessToast('改约成功')
  showReschedule.value = false
  load()
}

async function checkIn(r: Reservation) {
  await checkInReservation(r.id)
  showSuccessToast('开机成功')
  load()
}

onMounted(load)
</script>

<style scoped>
.submit-btn { margin: 12px 16px; }
.op-btn { margin-left: 6px; }
</style>
