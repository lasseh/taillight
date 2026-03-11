import { api } from '@/lib/api'
import { createVolumeDashboardStore } from '@/stores/volume-dashboard'

export const useDashboardStore = createVolumeDashboardStore('dashboard', api.getVolume, 'hosts')
