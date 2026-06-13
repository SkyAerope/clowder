<script setup lang="ts">
import { Skeleton } from '@/components/ui/skeleton'
import { TableBody, TableCell, TableRow } from '@/components/ui/table'

withDefaults(defineProps<{ cols: number; rows?: number }>(), { rows: 6 })
</script>

<template>
  <TableBody>
    <TableRow v-for="r in rows" :key="r">
      <TableCell v-for="c in cols" :key="c">
        <Skeleton class="h-4 w-full" />
      </TableCell>
    </TableRow>
  </TableBody>
</template>
