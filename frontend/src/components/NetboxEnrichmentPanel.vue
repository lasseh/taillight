<script setup lang="ts">
import type { NetboxLookup } from '@/types/netbox'

defineProps<{
  loading: boolean
  lookups: NetboxLookup[]
}>()

const typeLabel: Record<string, string> = {
  device: 'Device',
  ip: 'IP address',
  prefix: 'Prefix',
  asn: 'ASN',
  interface: 'Interface',
}
</script>

<template>
  <div class="bg-t-bg-dark border-t-border rounded border">
    <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
      Netbox
    </h3>

    <div v-if="loading" class="text-t-fg-dark px-4 py-3 text-xs">
      looking up entities in netbox...
    </div>

    <div v-else-if="lookups.length === 0" class="text-t-fg-dark px-4 py-3 text-xs">
      no recognizable entities in this message
    </div>

    <div
      v-for="(lk, i) in lookups"
      v-else
      :key="`${lk.entity.type}:${lk.entity.value}:${i}`"
      class="border-t-border border-b p-4 last:border-b-0"
    >
      <div class="mb-2 flex items-center gap-2">
        <span class="bg-t-bg text-t-fg-dark rounded px-1.5 py-0.5 text-xs uppercase">
          {{ typeLabel[lk.entity.type] ?? lk.entity.type }}
        </span>
        <span class="text-t-fg font-mono text-sm">{{ lk.entity.value }}</span>
        <span v-if="lk.entity.context?.device && lk.entity.type === 'interface'" class="text-t-fg-dark text-xs">
          on {{ lk.entity.context.device }}
        </span>
        <a
          v-if="lk.found && lk.data?.[lk.entity.type]?.url"
          :href="lk.data[lk.entity.type]?.url"
          target="_blank"
          rel="noopener"
          class="text-t-teal ml-auto text-xs hover:underline"
        >
          netbox &rarr;
        </a>
      </div>

      <div v-if="lk.error" class="text-t-yellow text-xs">
        lookup failed: {{ lk.error }}
      </div>

      <div v-else-if="!lk.found" class="text-t-fg-dark text-xs italic">
        not found in netbox
      </div>

      <dl v-else-if="lk.data?.device && lk.entity.type === 'device'" class="grid grid-cols-2 gap-x-6 gap-y-1 text-sm">
        <div v-if="lk.data.device.site"><dt class="text-t-fg-dark text-xs uppercase">Site</dt><dd class="text-t-fg">{{ lk.data.device.site }}</dd></div>
        <div v-if="lk.data.device.role"><dt class="text-t-fg-dark text-xs uppercase">Role</dt><dd class="text-t-fg">{{ lk.data.device.role }}</dd></div>
        <div v-if="lk.data.device.status"><dt class="text-t-fg-dark text-xs uppercase">Status</dt><dd class="text-t-fg">{{ lk.data.device.status }}</dd></div>
        <div v-if="lk.data.device.device_type"><dt class="text-t-fg-dark text-xs uppercase">Model</dt><dd class="text-t-fg">{{ lk.data.device.device_type }}</dd></div>
        <div v-if="lk.data.device.manufacturer"><dt class="text-t-fg-dark text-xs uppercase">Vendor</dt><dd class="text-t-fg">{{ lk.data.device.manufacturer }}</dd></div>
        <div v-if="lk.data.device.primary_ip"><dt class="text-t-fg-dark text-xs uppercase">Primary IP</dt><dd class="text-t-blue font-mono">{{ lk.data.device.primary_ip }}</dd></div>
        <div v-if="lk.data.device.description" class="col-span-2"><dt class="text-t-fg-dark text-xs uppercase">Description</dt><dd class="text-t-fg">{{ lk.data.device.description }}</dd></div>
      </dl>

      <dl v-else-if="lk.data?.ip && lk.entity.type === 'ip'" class="grid grid-cols-2 gap-x-6 gap-y-1 text-sm">
        <div v-if="lk.data.ip.status"><dt class="text-t-fg-dark text-xs uppercase">Status</dt><dd class="text-t-fg">{{ lk.data.ip.status }}</dd></div>
        <div v-if="lk.data.ip.role"><dt class="text-t-fg-dark text-xs uppercase">Role</dt><dd class="text-t-fg">{{ lk.data.ip.role }}</dd></div>
        <div v-if="lk.data.ip.dns_name"><dt class="text-t-fg-dark text-xs uppercase">DNS</dt><dd class="text-t-fg font-mono">{{ lk.data.ip.dns_name }}</dd></div>
        <div v-if="lk.data.ip.device"><dt class="text-t-fg-dark text-xs uppercase">Device</dt><dd class="text-t-teal font-mono">{{ lk.data.ip.device }}</dd></div>
        <div v-if="lk.data.ip.interface"><dt class="text-t-fg-dark text-xs uppercase">Interface</dt><dd class="text-t-teal font-mono">{{ lk.data.ip.interface }}</dd></div>
        <div v-if="lk.data.ip.vrf"><dt class="text-t-fg-dark text-xs uppercase">VRF</dt><dd class="text-t-fg">{{ lk.data.ip.vrf }}</dd></div>
        <div v-if="lk.data.ip.description" class="col-span-2"><dt class="text-t-fg-dark text-xs uppercase">Description</dt><dd class="text-t-fg">{{ lk.data.ip.description }}</dd></div>
      </dl>

      <dl v-else-if="lk.data?.prefix && lk.entity.type === 'prefix'" class="grid grid-cols-2 gap-x-6 gap-y-1 text-sm">
        <div v-if="lk.data.prefix.status"><dt class="text-t-fg-dark text-xs uppercase">Status</dt><dd class="text-t-fg">{{ lk.data.prefix.status }}</dd></div>
        <div v-if="lk.data.prefix.role"><dt class="text-t-fg-dark text-xs uppercase">Role</dt><dd class="text-t-fg">{{ lk.data.prefix.role }}</dd></div>
        <div v-if="lk.data.prefix.site"><dt class="text-t-fg-dark text-xs uppercase">Site</dt><dd class="text-t-fg">{{ lk.data.prefix.site }}</dd></div>
        <div v-if="lk.data.prefix.vlan"><dt class="text-t-fg-dark text-xs uppercase">VLAN</dt><dd class="text-t-fg">{{ lk.data.prefix.vlan }}</dd></div>
        <div v-if="lk.data.prefix.vrf"><dt class="text-t-fg-dark text-xs uppercase">VRF</dt><dd class="text-t-fg">{{ lk.data.prefix.vrf }}</dd></div>
        <div v-if="lk.data.prefix.description" class="col-span-2"><dt class="text-t-fg-dark text-xs uppercase">Description</dt><dd class="text-t-fg">{{ lk.data.prefix.description }}</dd></div>
      </dl>

      <dl v-else-if="lk.data?.asn && lk.entity.type === 'asn'" class="grid grid-cols-2 gap-x-6 gap-y-1 text-sm">
        <div v-if="lk.data.asn.rir"><dt class="text-t-fg-dark text-xs uppercase">RIR</dt><dd class="text-t-fg">{{ lk.data.asn.rir }}</dd></div>
        <div v-if="lk.data.asn.description" class="col-span-2"><dt class="text-t-fg-dark text-xs uppercase">Description</dt><dd class="text-t-fg">{{ lk.data.asn.description }}</dd></div>
      </dl>

      <dl v-else-if="lk.data?.interface && lk.entity.type === 'interface'" class="grid grid-cols-2 gap-x-6 gap-y-1 text-sm">
        <div v-if="lk.data.interface.device"><dt class="text-t-fg-dark text-xs uppercase">Device</dt><dd class="text-t-teal font-mono">{{ lk.data.interface.device }}</dd></div>
        <div v-if="lk.data.interface.type"><dt class="text-t-fg-dark text-xs uppercase">Type</dt><dd class="text-t-fg">{{ lk.data.interface.type }}</dd></div>
        <div v-if="lk.data.interface.mtu"><dt class="text-t-fg-dark text-xs uppercase">MTU</dt><dd class="text-t-fg font-mono">{{ lk.data.interface.mtu }}</dd></div>
        <div v-if="lk.data.interface.mac_address"><dt class="text-t-fg-dark text-xs uppercase">MAC</dt><dd class="text-t-fg font-mono">{{ lk.data.interface.mac_address }}</dd></div>
        <div v-if="lk.data.interface.lag"><dt class="text-t-fg-dark text-xs uppercase">LAG</dt><dd class="text-t-fg">{{ lk.data.interface.lag }}</dd></div>
        <div v-if="lk.data.interface.connected_endpoint"><dt class="text-t-fg-dark text-xs uppercase">Connected</dt><dd class="text-t-teal font-mono">{{ lk.data.interface.connected_endpoint }}</dd></div>
        <div v-if="lk.data.interface.enabled !== undefined"><dt class="text-t-fg-dark text-xs uppercase">Enabled</dt><dd class="text-t-fg">{{ lk.data.interface.enabled ? 'yes' : 'no' }}</dd></div>
        <div v-if="lk.data.interface.description" class="col-span-2"><dt class="text-t-fg-dark text-xs uppercase">Description</dt><dd class="text-t-fg">{{ lk.data.interface.description }}</dd></div>
      </dl>
    </div>
  </div>
</template>
