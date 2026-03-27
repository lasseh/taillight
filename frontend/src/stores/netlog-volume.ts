import { api } from '@/lib/api'
import { createVolumeStore } from '@/stores/volume-store'

export const useNetlogVolumeStore = createVolumeStore('netlog-volume', api.getNetlogVolume, 'hosts')
