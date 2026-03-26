import { api } from '@/lib/api'
import { createVolumeDashboardStore } from '@/stores/volume-dashboard'

export const useNetlogDashboardStore = createVolumeDashboardStore('netlog-dashboard', api.getNetlogVolume, 'hosts')
