import type { Port } from '../api'

export function isPortEnabled(port: Port): boolean {
  // Agents predating reservations returned only active mappings, without this field.
  return port.enabled !== false
}

function numbersOf(ports: readonly Port[]): number[] {
  return [...new Set(ports.map(port => port.number))].sort((a, b) => a - b)
}

function formatRanges(numbers: readonly number[]): string {
  const ranges: string[] = []
  let start = numbers[0]
  let end = start
  if (start === undefined) return '—'
  for (const number of numbers.slice(1)) {
    if (number === end + 1) {
      end = number
    } else {
      ranges.push(start === end ? String(start) : `${start}–${end}`)
      start = end = number
    }
  }
  ranges.push(start === end ? String(start) : `${start}–${end}`)
  return ranges.join('、')
}

export function summarizePorts(ports: readonly Port[]) {
  const numbers = numbersOf(ports)
  const tcp = ports.filter(port => port.proto === 'tcp')
  const udp = ports.filter(port => port.proto === 'udp')
  return {
    ranges: formatRanges(numbers),
    numberCount: numbers.length,
    enabledCount: ports.filter(isPortEnabled).length,
    tcpRanges: formatRanges(numbersOf(tcp)),
    udpRanges: formatRanges(numbersOf(udp)),
    hasUDP: udp.length > 0,
  }
}
