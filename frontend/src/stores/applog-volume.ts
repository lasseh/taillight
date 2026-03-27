import { api } from '@/lib/api'
import { createVolumeStore } from '@/stores/volume-store'

export const useAppLogVolumeStore = createVolumeStore('applog-volume', api.getAppLogVolume, 'services')
