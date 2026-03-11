import { api } from '@/lib/api'
import { createVolumeDashboardStore } from '@/stores/volume-dashboard'

export const useAppLogDashboardStore = createVolumeDashboardStore('applog-dashboard', api.getAppLogVolume, 'services')
