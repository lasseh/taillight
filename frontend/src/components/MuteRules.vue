<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiError } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import {
  DEFAULT_DURATION_MS,
  EXPIRY_PRESETS,
  hasMatchField,
  parseMutePrefill,
  type MutePrefill,
} from '@/lib/mute-rules'
import { useAuthStore } from '@/stores/auth'
import { useNetlogEventStore } from '@/stores/netlog-events'
import { useSrvlogEventStore } from '@/stores/srvlog-events'
import { useFocusTrap } from '@/composables/useFocusTrap'
import type { MuteFeed, MuteRule } from '@/types/mute'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const isAdmin = computed(() => auth.user?.is_admin === true)
const netlogEvents = useNetlogEventStore()
const srvlogEvents = useSrvlogEventStore()

// One list in server order (expires_at DESC): active rules plus rules that
// ended in the last 7 days.
const rules = ref<MuteRule[]>([])
const loading = ref(true)
const loadError = ref('')

// Create modal state.
const showModal = ref(false)
const saving = ref(false)
const saveError = ref('')

const formFeed = ref<MuteFeed>('netlog')
const formHostname = ref('')
const formProgramname = ref('')
const formSearch = ref('')
const formReason = ref('')
const formDurationMs = ref<number>(DEFAULT_DURATION_MS)

const modalEl = ref<HTMLElement | null>(null)
useFocusTrap(modalEl)

// Row actions: at most one open "expire in…" chooser or unmute confirmation.
const expireMenu = ref<number | null>(null)
const confirmUnmute = ref<number | null>(null)
const busy = ref<number | null>(null)
const actionError = ref('')

// Active/ended is derived from expires_at at render time; the list does not
// tick, so a rule that expires while the tab is open shows as ended on the
// next refetch.
function isEnded(rule: MuteRule): boolean {
  return new Date(rule.expires_at).getTime() <= Date.now()
}

function matchSummary(rule: MuteRule): string {
  return [rule.hostname, rule.programname, rule.search && `"${rule.search}"`]
    .filter(Boolean)
    .join(' · ')
}

function feedBadgeClass(feed: MuteFeed): string {
  return feed === 'netlog' ? 'bg-t-fuchsia/10 text-t-fuchsia' : 'bg-t-teal/10 text-t-teal'
}

async function fetchData() {
  try {
    const res = await api.listMuteRules()
    rules.value = res.data
    loadError.value = ''
  } catch (e) {
    loadError.value = e instanceof ApiError ? e.message : 'failed to load mute rules'
  } finally {
    loading.value = false
  }
}

// After any successful write, drop the rule's feed buffer so the feed
// refetches with current marking when the user returns to it.
async function afterMutation(feed: MuteFeed) {
  if (feed === 'netlog') netlogEvents.reset()
  else srvlogEvents.reset()
  await fetchData()
}

function openCreate(prefill?: MutePrefill) {
  formFeed.value = prefill?.feed ?? 'netlog'
  formHostname.value = prefill?.hostname ?? ''
  formProgramname.value = prefill?.programname ?? ''
  formSearch.value = prefill?.search ?? ''
  formReason.value = ''
  formDurationMs.value = DEFAULT_DURATION_MS
  saveError.value = ''
  showModal.value = true
}

function closeModal() {
  showModal.value = false
}

async function saveRule() {
  saveError.value = ''
  const feed = formFeed.value
  const match = {
    hostname: formHostname.value.trim(),
    programname: formProgramname.value.trim(),
    search: formSearch.value.trim(),
  }
  const reason = formReason.value.trim()
  if (!hasMatchField(match)) {
    saveError.value = 'set at least one of hostname, program or message'
    return
  }
  if (!reason) {
    saveError.value = 'reason is required'
    return
  }

  saving.value = true
  try {
    await api.createMuteRule({ feed, ...match, reason, duration_ms: formDurationMs.value })
    closeModal()
    await afterMutation(feed)
  } catch (e) {
    saveError.value = e instanceof ApiError ? e.message : 'failed to create mute rule'
  } finally {
    saving.value = false
  }
}

function openExpire(id: number) {
  confirmUnmute.value = null
  expireMenu.value = id
}

function openUnmute(id: number) {
  expireMenu.value = null
  confirmUnmute.value = id
}

async function expireIn(rule: MuteRule, durationMs: number) {
  actionError.value = ''
  busy.value = rule.id
  try {
    await api.setMuteRuleExpiry(rule.id, durationMs)
    expireMenu.value = null
    await afterMutation(rule.feed)
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : 'failed to update mute rule'
  } finally {
    busy.value = null
  }
}

async function unmute(rule: MuteRule) {
  actionError.value = ''
  busy.value = rule.id
  try {
    await api.endMuteRule(rule.id)
    confirmUnmute.value = null
    await afterMutation(rule.feed)
  } catch (e) {
    actionError.value = e instanceof ApiError ? e.message : 'failed to unmute'
  } finally {
    busy.value = null
  }
}

onMounted(() => {
  // "Mute similar" arrives with mute_* keys: open the prefilled form, then
  // strip the keys so a reload doesn't reopen it.
  const prefill = parseMutePrefill(route.query)
  if (prefill) {
    if (isAdmin.value) openCreate(prefill)
    router.replace({ query: { tab: 'mutes' } })
  }
  fetchData()
})
</script>

<template>
  <div class="space-y-4">
    <!-- Header -->
    <div class="flex items-center justify-between gap-3">
      <p class="text-t-fg-dark text-sm">
        mute rules hide matching events from the feeds and keep them out of notifications
      </p>
      <button
        v-if="isAdmin"
        class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border shrink-0 border px-4 py-2 text-sm transition-all"
        @click="openCreate()"
      >
        + add mute
      </button>
    </div>

    <!-- Loading / error -->
    <div v-if="loading" class="text-t-fg-dark py-10 text-center text-sm">loading...</div>
    <div v-else-if="loadError" class="text-t-red py-10 text-center text-sm">{{ loadError }}</div>

    <template v-else>
      <div v-if="actionError" class="text-t-red text-sm px-5 py-2">{{ actionError }}</div>

      <!-- Mutes list -->
      <div class="bg-t-bg-dark border-t-border rounded border">
        <div class="border-t-border border-b px-5 py-2.5">
          <h3 class="text-t-fg-dark text-xs font-semibold uppercase tracking-wide">
            Mutes
            <span class="text-t-fg-gutter ml-1 font-normal normal-case">{{ rules.length }}</span>
          </h3>
        </div>

        <div class="divide-t-border divide-y">
          <div
            v-for="rule in rules"
            :key="rule.id"
            class="hover:bg-t-bg-hover flex flex-col gap-2 px-5 py-3 text-sm transition-colors sm:flex-row sm:items-start"
            :class="{ 'opacity-50': isEnded(rule) }"
          >
            <div class="min-w-0 flex-1 space-y-1">
              <div class="flex flex-wrap items-center gap-2">
                <span
                  class="inline-block rounded px-1.5 py-0.5 text-xs uppercase"
                  :class="feedBadgeClass(rule.feed)"
                >
                  {{ rule.feed }}
                </span>
                <span class="text-t-fg min-w-0 break-all font-medium">{{
                  matchSummary(rule)
                }}</span>
                <span v-if="isEnded(rule)" class="text-t-fg-gutter text-xs">ended</span>
              </div>
              <p class="text-t-fg-dark break-words">{{ rule.reason }}</p>
              <p class="text-t-fg-gutter text-xs">
                #{{ rule.id }} · by {{ rule.created_by }} · created
                {{ formatDateTime(rule.created_at) }} ·
                {{ isEnded(rule) ? 'ended' : 'expires' }}
                {{ formatDateTime(rule.expires_at) }}
              </p>
            </div>

            <div v-if="isAdmin" class="flex shrink-0 flex-wrap items-center gap-3 sm:justify-end">
              <template v-if="expireMenu === rule.id">
                <span class="text-t-fg-dark text-xs">expire in</span>
                <button
                  v-for="p in EXPIRY_PRESETS"
                  :key="p.label"
                  class="text-t-blue/70 hover:text-t-blue text-xs transition-colors"
                  :disabled="busy === rule.id"
                  @click="expireIn(rule, p.ms)"
                >
                  {{ p.label }}
                </button>
                <button class="text-t-fg-dark hover:text-t-fg text-xs" @click="expireMenu = null">
                  cancel
                </button>
              </template>
              <template v-else-if="confirmUnmute === rule.id">
                <span class="text-t-fg-dark text-xs">unmute?</span>
                <button
                  class="text-t-red hover:brightness-125 text-xs font-semibold"
                  :disabled="busy === rule.id"
                  @click="unmute(rule)"
                >
                  yes
                </button>
                <button
                  class="text-t-fg-dark hover:text-t-fg text-xs"
                  @click="confirmUnmute = null"
                >
                  no
                </button>
              </template>
              <template v-else>
                <button
                  class="text-t-blue/70 hover:text-t-blue text-xs transition-colors"
                  @click="openExpire(rule.id)"
                >
                  expire in…
                </button>
                <button
                  v-if="!isEnded(rule)"
                  class="text-t-red/70 hover:text-t-red text-xs transition-colors"
                  @click="openUnmute(rule.id)"
                >
                  unmute
                </button>
              </template>
            </div>
          </div>
        </div>

        <!-- Empty state -->
        <div v-if="rules.length === 0" class="px-5 py-10 text-center">
          <p class="text-t-fg-dark text-sm">no mute rules active or ended in the last 7 days</p>
          <button
            v-if="isAdmin"
            class="text-t-yellow mt-2 text-sm hover:brightness-125"
            @click="openCreate()"
          >
            create your first mute
          </button>
        </div>
      </div>
    </template>

    <!-- Modal overlay -->
    <Teleport to="body">
      <Transition name="modal">
        <div
          v-if="showModal"
          class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/50 pt-10 pb-10"
          @click.self="closeModal"
        >
          <div
            ref="modalEl"
            class="bg-t-bg-dark border-t-border w-full max-w-2xl rounded border shadow-xl"
          >
            <!-- Modal header -->
            <div class="border-t-border flex items-center justify-between border-b px-5 py-3">
              <h3 class="text-t-fg text-sm font-semibold">Add Mute</h3>
              <button class="text-t-fg-dark hover:text-t-fg text-xs" @click="closeModal">
                close
              </button>
            </div>

            <!-- Modal body -->
            <div class="space-y-4 px-5 py-4">
              <!-- Feed -->
              <div>
                <span class="text-t-fg-dark text-sm">Feed</span>
                <div class="mt-1.5 flex gap-2">
                  <button
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="
                      formFeed === 'netlog'
                        ? 'border-t-fuchsia text-t-fuchsia'
                        : 'border-t-border text-t-fg-dark hover:text-t-fg'
                    "
                    @click="formFeed = 'netlog'"
                  >
                    Netlog
                  </button>
                  <button
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="
                      formFeed === 'srvlog'
                        ? 'border-t-teal text-t-teal'
                        : 'border-t-border text-t-fg-dark hover:text-t-fg'
                    "
                    @click="formFeed = 'srvlog'"
                  >
                    Srvlog
                  </button>
                </div>
              </div>

              <!-- Match fields -->
              <div class="border-t-border space-y-3 border-t pt-3">
                <div>
                  <span class="text-t-fg-dark text-xs font-semibold uppercase tracking-wider"
                    >Match</span
                  >
                  <p class="text-t-fg-gutter mt-1 text-xs">
                    events matching every set field are muted; set at least one
                  </p>
                </div>
                <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <label class="block">
                    <span class="text-t-fg-dark text-xs">Hostname</span>
                    <input
                      v-model="formHostname"
                      type="text"
                      placeholder="router* or exact"
                      class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    />
                  </label>
                  <label class="block">
                    <span class="text-t-fg-dark text-xs">Program</span>
                    <input
                      v-model="formProgramname"
                      type="text"
                      placeholder="exact, e.g. rpd"
                      class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                    />
                  </label>
                </div>
                <label class="block">
                  <span class="text-t-fg-dark text-xs">Message contains</span>
                  <input
                    v-model="formSearch"
                    type="text"
                    placeholder="case-insensitive"
                    class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-2 py-1.5 text-sm outline-none"
                  />
                  <span class="text-t-fg-gutter text-xs"
                    >trim to the stable part, e.g. bundle ae30</span
                  >
                </label>
              </div>

              <!-- Reason -->
              <label class="block">
                <span class="text-t-fg-dark text-sm">Reason</span>
                <input
                  v-model="formReason"
                  type="text"
                  placeholder="e.g. known LACP flap, fix scheduled"
                  class="bg-t-bg border-t-border text-t-fg placeholder:text-t-fg-gutter focus:border-t-yellow mt-1 block w-full border px-3 py-2 text-sm outline-none"
                />
              </label>

              <!-- Expiry -->
              <div class="border-t-border space-y-2 border-t pt-3">
                <span class="text-t-fg-dark text-xs font-semibold uppercase tracking-wider"
                  >Expires in</span
                >
                <div class="flex flex-wrap gap-2">
                  <button
                    v-for="p in EXPIRY_PRESETS"
                    :key="p.label"
                    class="border px-3 py-1.5 text-sm transition-all"
                    :class="
                      formDurationMs === p.ms
                        ? 'border-t-yellow text-t-yellow'
                        : 'border-t-border text-t-fg-dark hover:text-t-fg hover:border-t-fg-dark'
                    "
                    @click="formDurationMs = p.ms"
                  >
                    {{ p.label }}
                  </button>
                </div>
              </div>
            </div>

            <!-- Modal footer -->
            <div class="border-t-border flex flex-wrap items-center gap-3 border-t px-5 py-3">
              <button
                class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
                :disabled="saving"
                @click="saveRule"
              >
                {{ saving ? 'saving...' : 'create mute' }}
              </button>
              <button
                class="text-t-fg-dark hover:text-t-fg text-sm transition-colors"
                @click="closeModal"
              >
                cancel
              </button>
              <span v-if="saveError" class="text-t-red text-sm">{{ saveError }}</span>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.modal-enter-active,
.modal-leave-active {
  transition: opacity 0.15s ease;
}

.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
</style>
