package bloomfilter

import (
    "encoding/binary"
    "math"
    "sync"
)

type BloomFilter struct {
    bits    []uint64
    numBits uint
    numHash uint
    count   uint
    mutex   sync.RWMutex
}

// NewBloomFilter cria um novo filtro com base no número esperado de itens e taxa de falsos positivos
func NewBloomFilter(expectedItems uint, falsePositiveRate float64) *BloomFilter {
    numBits := uint(-float64(expectedItems) * math.Log(falsePositiveRate) / (math.Log(2) * math.Log(2)))
    numHash := uint(float64(numBits) / float64(expectedItems) * math.Log(2))
    
    return &BloomFilter{
        bits:    make([]uint64, (numBits+63)/64), // Arredonda para cima para o próximo múltiplo de 64
        numBits: numBits,
        numHash: numHash,
        count:   0,
    }
}

// Add adiciona um item ao filtro
func (bf *BloomFilter) Add(data []byte) {
    bf.mutex.Lock()
    defer bf.mutex.Unlock()

    h1, h2 := bf.hash(data)
    
    for i := uint(0); i < bf.numHash; i++ {
        pos := bf.getPosition(h1, h2, i)
        arrayPos := pos / 64
        bitPos := pos % 64
        bf.bits[arrayPos] |= 1 << bitPos
    }
    
    bf.count++
}

// Contains verifica se um item possivelmente existe no filtro
func (bf *BloomFilter) Contains(data []byte) bool {
    bf.mutex.RLock()
    defer bf.mutex.RUnlock()

    h1, h2 := bf.hash(data)
    
    for i := uint(0); i < bf.numHash; i++ {
        pos := bf.getPosition(h1, h2, i)
        arrayPos := pos / 64
        bitPos := pos % 64
        if (bf.bits[arrayPos] & (1 << bitPos)) == 0 {
            return false
        }
    }
    
    return true
}

// hash implementa um double hashing eficiente
func (bf *BloomFilter) hash(data []byte) (uint64, uint64) {
    const (
        m1 = 0xc6a4a7935bd1e995
        m2 = 0x5555555555555555
        r1 = 47
        r2 = 23
    )

    var h1, h2 uint64
    
    length := len(data)
    remaining := length % 8
    end := length - remaining
    
    for i := 0; i < end; i += 8 {
        k := binary.LittleEndian.Uint64(data[i:])
        k *= m1
        k ^= k >> r1
        k *= m2
        h1 ^= k
        h1 *= m1
        
        k *= m2
        k ^= k >> r2
        k *= m1
        h2 ^= k
        h2 *= m2
    }
    
    if remaining > 0 {
        var k1, k2 uint64
        for i := 0; i < remaining; i++ {
            k1 = (k1 << 8) | uint64(data[end+i])
            k2 = (k2 << 8) | uint64(data[end+i])
        }
        h1 ^= k1 * m1
        h2 ^= k2 * m2
    }
    
    h1 ^= uint64(length)
    h2 ^= uint64(length)
    
    h1 *= m1
    h2 *= m2
    
    return h1, h2
}

// getPosition calcula a posição do bit usando double hashing
func (bf *BloomFilter) getPosition(h1, h2 uint64, i uint) uint {
    pos := h1 + (h2 * uint64(i))
    return uint(pos % uint64(bf.numBits))
}

// Clear limpa o filtro
func (bf *BloomFilter) Clear() {
    bf.mutex.Lock()
    defer bf.mutex.Unlock()
    
    for i := range bf.bits {
        bf.bits[i] = 0
    }
    bf.count = 0
}

// GetCount retorna o número de elementos adicionados
func (bf *BloomFilter) GetCount() uint {
    bf.mutex.RLock()
    defer bf.mutex.RUnlock()
    return bf.count
}

// GetNumBits retorna o número total de bits no filtro
func (bf *BloomFilter) GetNumBits() uint {
    return bf.numBits
}

// GetNumHash retorna o número de funções hash
func (bf *BloomFilter) GetNumHash() uint {
    return bf.numHash
}

// EstimateFalsePositiveRate estima a taxa atual de falsos positivos
func (bf *BloomFilter) EstimateFalsePositiveRate() float64 {
    bf.mutex.RLock()
    defer bf.mutex.RUnlock()
    
    return math.Pow(1-math.Exp(-float64(bf.numHash)*float64(bf.count)/float64(bf.numBits)), float64(bf.numHash))
}
