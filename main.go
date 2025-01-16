package main

import (
    "bytes"
  
    "encoding/json"
    "fmt"
    "math/big"
    "os"
    "crypto/rand"    // Adicione esta linha
    "math"       // Adicione esta linha
    "runtime"
    "strings"
    "sync"
    "sync/atomic"
    "time"
    "btchunt/wif"
    "btchunt/bloom"
)



const (
    minBallSize     = 1000     // Tamanho mínimo da bola de verificação
    maxBallSize     = 10000    // Tamanho máximo da bola de verificação
    defaultBallSize = 5000     // Tamanho inicial da bola de verificação
)

const (
    initialVelocity     = 0.4    // Velocidade inicial
    baseEnergyLoss      = 0.015  // Perda de energia base nas colisões
    adaptiveEnergyLoss  = 0.06   // Perda de energia adaptativa
    minVelocity         = 0.008  // Velocidade mínima permitida
    maxVelocity         = 0.7    // Velocidade máxima permitida
    momentumFactor      = 0.25   // Fator de conservação de momentum
    adaptiveRangeFactor = 0.2    // Fator de adaptação do range
)
var base58Alphabet = []byte("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")

// RangeConfig define a estrutura do arquivo JSON
type RangeConfig struct {
    Ranges []struct {
        Min    string `json:"min"`
        Max    string `json:"max"`
        Status string `json:"status"`
    } `json:"ranges"`
}


// Range defines the search interval
type Range struct {
    Min    *big.Int
    Max    *big.Int
    Status []byte // Modificado para armazenar diretamente o hash160
    Filter *bloomfilter.BloomFilter
}

// Stats mantém as estatísticas da busca
type Stats struct {
    keysChecked     uint64
    startTime       time.Time
    lastSpeedUpdate time.Time
    searchRange     Range
}



// BallRange define o tamanho da "bola" de verificação
type BallRange struct {
    current *big.Int
    size    int64
}

// JumpStrategy mantém o estado da estratégia de saltos
type JumpStrategy struct {
    id            int
    bounceCount   uint64
    lastJump      *big.Int
    direction     int
    velocity      float64
    energy        float64
    ballRange     BallRange
    successRate   float64
    lastPositions [10]*big.Int
    coverage      map[string]uint64
    momentum      float64
    mu            sync.Mutex
    height        float64   // Altura atual da trajetória
    groundTouches int      // Contador de toques no chão
    lastBounce    time.Time // Tempo do último toque
    energyLevel   float64   // Nível de energia atual
}



// base58Decode decodes Base58 data
func base58Decode(input string) []byte {
    result := big.NewInt(0)
    base := big.NewInt(58)

    for _, char := range []byte(input) {
        value := bytes.IndexByte(base58Alphabet, char)
        if value == -1 {
            panic("Invalid Base58 character")
        }
        result.Mul(result, base)
        result.Add(result, big.NewInt(int64(value)))
    }

    decoded := result.Bytes()

    // Add leading zeros
    leadingZeros := 0
    for _, char := range []byte(input) {
        if char != base58Alphabet[0] {
            break
        }
        leadingZeros++
    }

    return append(make([]byte, leadingZeros), decoded...)
}

// addressToHash160 converte endereço Bitcoin para hash160
func addressToHash160(address string) []byte {
    decoded := base58Decode(address)
    // Remove version byte and checksum
    return decoded[1 : len(decoded)-4]
}

// GenerateSmartJump gera saltos adaptativos baseados em estratégia matemática
func GenerateSmartJump(rangeSize *big.Int, strategy *JumpStrategy) *big.Int {
    if strategy == nil {
        // Tratamento de erro para caso strategy seja nil
        return new(big.Int).SetInt64(10000) // valor padrão de fallback
    }

    // Atualiza a cobertura
    strategy.updateCoverage(strategy.ballRange.current)
    
    // Resto da implementação...
    jump := strategy.calculateAdaptiveJump(rangeSize)
    
    if strategy.bounceCount > 0 {
        energyLoss := baseEnergyLoss + (adaptiveEnergyLoss * (1.0 - strategy.successRate))
        strategy.energy *= (1.0 - energyLoss)
    }

    // Atualiza histórico de posições
    for i := len(strategy.lastPositions)-1; i > 0; i-- {
        if strategy.lastPositions[i] == nil {
            strategy.lastPositions[i] = new(big.Int)
        }
        strategy.lastPositions[i].Set(strategy.lastPositions[i-1])
    }
    
    if strategy.lastPositions[0] == nil {
        strategy.lastPositions[0] = new(big.Int)
    }
    strategy.lastPositions[0].Set(strategy.ballRange.current)

    strategy.lastJump = new(big.Int).Set(jump)
    return jump
}

// LoadRangesFromJSON carrega as configurações do arquivo JSON
func LoadRangesFromJSON(filename string) ([]Range, error) {
    data, err := os.ReadFile(filename)
    if err != nil {
        return nil, fmt.Errorf("error reading file: %v", err)
    }

    var config RangeConfig
    if err := json.Unmarshal(data, &config); err != nil {
        return nil, fmt.Errorf("error parsing JSON: %v", err)
    }

    ranges := make([]Range, len(config.Ranges))
    for i, r := range config.Ranges {
        minStr := strings.TrimPrefix(r.Min, "0x")
        maxStr := strings.TrimPrefix(r.Max, "0x")

        min, success := new(big.Int).SetString(minStr, 16)
        if !success {
            return nil, fmt.Errorf("invalid min value: %s", r.Min)
        }

        max, success := new(big.Int).SetString(maxStr, 16)
        if !success {
            return nil, fmt.Errorf("invalid max value: %s", r.Max)
        }

        // Converte o endereço para hash160
        hash160 := addressToHash160(r.Status)

        // Inicializa o Bloom Filter
        expectedItems := uint(500 * 1024 * 1024 * 8 / 12)
        falsePositiveRate := 0.0000001
        bf := bloomfilter.NewBloomFilter(expectedItems, falsePositiveRate)
        bf.Add(hash160)

        ranges[i] = Range{
            Min:    min,
            Max:    max,
            Status: hash160,
            Filter: bf,
        }
    }

    return ranges, nil
}

// PrintProgress imprime as estatísticas de progresso
func PrintProgress(stats *Stats) {
    const barWidth = 100

    for {
        select {
        case <-done:
            return
        default:
            time.Sleep(5 * time.Second)
            fmt.Print("\033[H\033[2J")

            rangeSize := new(big.Int).Sub(stats.searchRange.Max, stats.searchRange.Min)
            
            keysChecked := atomic.LoadUint64(&stats.keysChecked)
            duration := time.Since(stats.startTime)
            keysPerSecond := float64(keysChecked) / duration.Seconds()

            globalStatus.mu.Lock()
            currentPos := new(big.Int).Set(globalStatus.currentPosition)
            globalStatus.mu.Unlock()

            progress := new(big.Float).Quo(
                new(big.Float).SetInt(new(big.Int).Sub(currentPos, stats.searchRange.Min)),
                new(big.Float).SetInt(rangeSize),
            )
            percentage, _ := progress.Float64()
            percentage *= 100

            remainingKeys := new(big.Int).Sub(stats.searchRange.Max, currentPos)
            remainingKeysFloat := new(big.Float).SetInt(remainingKeys)
            remainingKeysVal, _ := remainingKeysFloat.Float64()
            estimatedTimeRemaining := remainingKeysVal / keysPerSecond

            progressBar := generateProgressBar(
                stats.searchRange.Min,
                stats.searchRange.Max,
                barWidth,
            )

            fmt.Printf(
                "Progress Report\n" +
                "================\n" +
                "Range Size (keys): %s\n" +
                "CPU Cores Used   : %d\n" +
                "\n" +
                "Search Progress  : \n" +
                "[%s] %.2f%%\n" +
                "\n" +
                "Keys Checked     : %d\n" +
                "Keys/sec         : %.2f\n" +
                "Time Elapsed     : %s\n" +
                "Est. Time Left   : %s\n" +
                "Current Position : %x\n" +
                "Range Start     : %x\n" +
                "Range End       : %x\n",
                rangeSize.Text(10),
                runtime.NumCPU(),
                progressBar,
                percentage,
                keysChecked,
                keysPerSecond,
                duration.Round(time.Second),
                time.Duration(estimatedTimeRemaining * float64(time.Second)).Round(time.Second),
                currentPos,
                stats.searchRange.Min,
                stats.searchRange.Max,
            )
        }
    }
}

func shouldSkipKey(key *big.Int) bool {
    bytes := key.Bytes()
    if len(bytes) < 2 { // Menos de 4 chars hex
        return false
    }

    var count byte = 1
    var last byte = bytes[0] & 0xF // Pega apenas 4 bits
    
    for _, b := range bytes {
        high := b >> 4  // 4 bits mais significativos
        if high == last && high != 0 {
            count++
            if count >= 4 {
                return true
            }
        } else {
            count = 1
        }
        last = high

        low := b & 0xF  // 4 bits menos significativos
        if low == last && low != 0 {
            count++
            if count >= 4 {
                return true
            }
        } else {
            count = 1
        }
        last = low
    }

    return false
}

// CheckAddressWithWIF verifica se uma chave privada corresponde ao hash160 alvo
func CheckAddressWithWIF(privateKey *big.Int, r Range) bool {
    privKeyBytes := privateKey.FillBytes(make([]byte, 32))
    pubKey := wif.GeneratePublicKey(privKeyBytes)
    hash160 := wif.Hash160(pubKey)
    
    // Primeiro verifica no Bloom Filter
    if !r.Filter.Contains(hash160) {
        return false
    }
    
    // Se passar pelo Bloom Filter, faz a verificação final
    return bytes.Equal(hash160, r.Status)
}

// Adicione esta estrutura global para manter a posição atual da bola
type GlobalStatus struct {
    positions      map[int]*big.Int  // map de id da thread para posição
    currentPosition *big.Int         // mantenha isso para compatibilidade
    mu             sync.Mutex
}

var globalStatus = GlobalStatus{
    positions:       make(map[int]*big.Int),
    currentPosition: new(big.Int),
}

// Função para atualizar a posição global
func updateGlobalPosition(position *big.Int) {
    globalStatus.mu.Lock()
    defer globalStatus.mu.Unlock()
    globalStatus.currentPosition.Set(position)
    // Também atualiza o mapa de posições
    if globalStatus.positions == nil {
        globalStatus.positions = make(map[int]*big.Int)
    }
    if globalStatus.positions[0] == nil {
        globalStatus.positions[0] = new(big.Int)
    }
    globalStatus.positions[0].Set(position)
}

// Função para gerar a barra visual
func generateProgressBar(min, max *big.Int, width int) string {
    var builder strings.Builder
    builder.Grow(width)
    
    globalStatus.mu.Lock()
    // Usa apenas a posição da thread 0
    currentPos := new(big.Int)
    if globalStatus.positions[0] != nil {
        currentPos.Set(globalStatus.positions[0])
    } else {
        currentPos.Set(min)
    }
    globalStatus.mu.Unlock()

    if currentPos.Cmp(min) < 0 || currentPos.Cmp(max) > 0 {
        return strings.Repeat("-", width)
    }

    // Calcula a posição relativa usando big.Float para maior precisão
    range_size := new(big.Float).SetInt(new(big.Int).Sub(max, min))
    current_pos := new(big.Float).SetInt(new(big.Int).Sub(currentPos, min))
    
    // Calcula a posição como uma fração do range total
    ratio := new(big.Float).Quo(current_pos, range_size)
    floatVal, _ := ratio.Float64()
    
    // Calcula a posição na barra
    position := int(floatVal * float64(width))

    // Garante que a posição está dentro dos limites
    if position >= width {
        position = width - 1
    }
    if position < 0 {
        position = 0
    }

    // Constrói a barra
    for i := 0; i < width; i++ {
        switch {
        case i == position:
            builder.WriteRune('⬤') // Bola sólida
        case i < position:
            builder.WriteRune('━') // Área percorrida
        default:
            builder.WriteRune('─') // Área não percorrida
        }
    }

    return builder.String()
}

// SearchWorker implementa a lógica de busca para cada worker
func SearchWorker(
    id int,
    r Range,
    startPoint *big.Int,
    direction int,
    stats *Stats,
    found chan *big.Int,
    wg *sync.WaitGroup,
) {
    defer wg.Done()
    
    strategy := NewJumpStrategy(startPoint)
    strategy.direction = direction
    strategy.energyLevel = 1.0
    strategy.lastBounce = time.Now()
    
    ball := new(big.Int).Set(startPoint)
    rangeSize := new(big.Int).Sub(r.Max, r.Min)
    currentKey := new(big.Int)
    
    checkBuffer := make([]*big.Int, 0, strategy.ballRange.size)
    lastBounceTime := time.Now()
    bounceCount := 0

    for {
        select {
        case <-done:
            return
        default:
            jump := GenerateSmartJump(rangeSize, strategy)
            
            if strategy.direction > 0 {
                ball.Add(ball, jump)
                if ball.Cmp(r.Max) > 0 {
                    excess := new(big.Int).Sub(ball, r.Max)
                    ball.Sub(r.Max, excess)
                    strategy.direction *= -1
                    lastBounceTime = time.Now()
                    bounceCount++
                    strategy.energyLevel *= (1.0 - baseEnergyLoss)
                    atomic.AddUint64(&strategy.bounceCount, 1)
                }
            } else {
                ball.Sub(ball, jump)
                if ball.Cmp(r.Min) < 0 {
                    deficit := new(big.Int).Sub(r.Min, ball)
                    ball.Add(r.Min, deficit)
                    strategy.direction *= -1
                    lastBounceTime = time.Now()
                    bounceCount++
                    strategy.energyLevel *= (1.0 - baseEnergyLoss)
                    atomic.AddUint64(&strategy.bounceCount, 1)
                }
            }

            updateGlobalPosition(ball)
            strategy.ballRange.current.Set(ball)
            checkBuffer = checkBuffer[:0]
            
            currentKey.Set(ball)
            batchSize := strategy.ballRange.size
            atomic.AddUint64(&stats.keysChecked, uint64(batchSize)) 
            
            for i := int64(0); i < strategy.ballRange.size; i++ {
                if currentKey.Cmp(r.Max) > 0 || currentKey.Cmp(r.Min) < 0 {
                    break
                } 

                if !shouldSkipKey(currentKey) {
                    checkBuffer = append(checkBuffer, new(big.Int).Set(currentKey))
                }
                currentKey.Add(currentKey, big.NewInt(1))
            }

            for _, keyToCheck := range checkBuffer {
                select {
                case <-done:
                    return
                default:
                    if CheckAddressWithWIF(keyToCheck, r) {
                        select {
                        case found <- new(big.Int).Set(keyToCheck):
                            return
                        default:
                            return
                        }
                    }
                }
            }

            timeSinceLastBounce := time.Since(lastBounceTime).Seconds()
            strategy.height = strategy.energyLevel * math.Sin(timeSinceLastBounce*math.Pi)
            strategy.velocity = strategy.energyLevel * (1.0 + strategy.height)
            
            if strategy.energyLevel < minVelocity {
                strategy.energyLevel = minVelocity
            }

            strategy.updateCoverage(ball)
            strategy.adjustBallRange()

            momentum := float64(strategy.direction) * strategy.velocity
            strategy.momentum = strategy.momentum*momentumFactor + momentum*(1-momentumFactor)

            for i := len(strategy.lastPositions)-1; i > 0; i-- {
                if strategy.lastPositions[i] == nil {
                    strategy.lastPositions[i] = new(big.Int)
                }
                strategy.lastPositions[i].Set(strategy.lastPositions[i-1])
            }
            strategy.lastPositions[0].Set(ball)
        }
    }
}


func (js *JumpStrategy) calculateAdaptiveJump(rangeSize *big.Int) *big.Int {
    js.mu.Lock()
    defer js.mu.Unlock()

    if js.coverage == nil {
        js.coverage = make(map[string]uint64)
    }

    currentSegment := new(big.Int).Div(js.ballRange.current, big.NewInt(1000000))
    localDensity := float64(js.coverage[currentSegment.String()]) / 1000.0

    // Calcula o tamanho do salto baseado na energia
    rangeSizeFloat := new(big.Float).SetInt(rangeSize)
    jumpSize := new(big.Float).Mul(rangeSizeFloat, big.NewFloat(js.energyLevel))

    // Simula a trajetória parabólica
    timeSinceLastBounce := time.Since(js.lastBounce).Seconds()
    heightFactor := math.Sin(timeSinceLastBounce * math.Pi)
    js.height = js.energyLevel * heightFactor

    // Ajusta o tamanho do salto baseado na altura e densidade local
    velocityAdjustment := 1.0 - (localDensity * 0.1)
    if velocityAdjustment < 0.1 {
        velocityAdjustment = 0.1
    }

    jumpSize = new(big.Float).Mul(jumpSize, big.NewFloat(1.0+js.height))
    jumpSize = new(big.Float).Mul(jumpSize, big.NewFloat(velocityAdjustment))

    // Adiciona um elemento de aleatoriedade
    randomness, _ := rand.Int(rand.Reader, big.NewInt(100))
    randomFactor := 1.0 + (float64(randomness.Int64()%50) - 25.0) / 1000.0
    jumpSize = new(big.Float).Mul(jumpSize, big.NewFloat(randomFactor))

    // Converte para inteiro
    jumpInt, _ := jumpSize.Int(nil)
    
    // Garante tamanho mínimo do salto
    if jumpInt.Cmp(big.NewInt(minBallSize)) < 0 {
        jumpInt.SetInt64(minBallSize)
    }

    return jumpInt
}

func (js *JumpStrategy) updateCoverage(position *big.Int) {
    js.mu.Lock()
    defer js.mu.Unlock()

    if js.coverage == nil {
        js.coverage = make(map[string]uint64)
    }

    segment := new(big.Int).Div(position, big.NewInt(1000000))
    key := segment.String()
    js.coverage[key]++
}

func (js *JumpStrategy) adjustBallRange() {
    js.mu.Lock()
    defer js.mu.Unlock()
    
    // Ajusta o tamanho do range baseado na taxa de sucesso
    newSize := int64(float64(js.ballRange.size) * (1.0 + (js.successRate-0.5)*adaptiveRangeFactor))
    
    // Limita o tamanho do range
    if newSize < minBallSize {
        newSize = minBallSize
    } else if newSize > maxBallSize {
        newSize = maxBallSize
    }
    
    js.ballRange.size = newSize
}
func NewJumpStrategy(initialPosition *big.Int) *JumpStrategy {
    var lastPositions [10]*big.Int
    for i := range lastPositions {
        lastPositions[i] = new(big.Int).Set(initialPosition)
    }

    return &JumpStrategy{
        direction:     1,
        velocity:      initialVelocity,
        energy:        1.0,
        momentum:      initialVelocity,
        successRate:   1.0,
        lastJump:      new(big.Int),
        lastPositions: lastPositions,
        coverage:      make(map[string]uint64),
        ballRange: BallRange{
            current: new(big.Int).Set(initialPosition),
            size:    defaultBallSize,
        },
        height:        1.0,
        energyLevel:   1.0,
        groundTouches: 0,
        lastBounce:    time.Now(),
    }
}

// Search implementa a busca principal
var done = make(chan struct{}) // Adicione esta variável global

func Search(r Range) {
    found := make(chan *big.Int, 1)
    var wg sync.WaitGroup
    
    stats := &Stats{
        startTime: time.Now(),
        lastSpeedUpdate: time.Now(),
        searchRange: r,
    }

    rangeSize := new(big.Int).Sub(r.Max, r.Min)

    // Inicia o monitoramento de progresso em uma goroutine separada
    progressDone := make(chan struct{})
    go func() {
        PrintProgress(stats)
        close(progressDone)
    }()

    numCPU := runtime.NumCPU()*6
    
    for i := 0; i < numCPU; i++ {
        wg.Add(1)
        
        offset := new(big.Int).Mul(
            rangeSize,
            big.NewInt(int64(i)),
        )
        offset.Div(offset, big.NewInt(int64(numCPU)))
        
        startPoint := new(big.Int).Add(r.Min, offset)
        direction := 1
        if i%2 == 1 {
            direction = -1
        }

        go SearchWorker(i, r, startPoint, direction, stats, found, &wg)
    }

    // Espera por uma chave encontrada
    foundKey := <-found
    close(done) // Sinaliza para todas as goroutines pararem

    // Limpa a tela uma última vez
    fmt.Print("\033[H\033[2J")

    // Processa a chave encontrada
    privKeyBytes := foundKey.FillBytes(make([]byte, 32))
    pubKey := wif.GeneratePublicKey(privKeyBytes)
    wifKey := wif.PrivateKeyToWIF(foundKey)
    address := wif.PublicKeyToAddress(pubKey)
    
    saveFoundKeyDetails(foundKey, wifKey, address)

    // Espera todas as goroutines terminarem
    wg.Wait()
    close(found)
}

// saveFoundKeyDetails salva os detalhes da chave privada encontrada em um arquivo
func saveFoundKeyDetails(privKey *big.Int, wifKey, address string) {
	fmt.Println("-------------------CHAVE ENCONTRADA!!!!-------------------")
	fmt.Printf("Private key: %064x\n", privKey)
	fmt.Printf("WIF: %s\n", wifKey)
	fmt.Printf("Endereço: %s\n", address)

	file, err := os.OpenFile("found_keys.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Erro ao salvar chave encontrada: %v\n", err)
		return
	}
	defer file.Close()

	_, err = file.WriteString(fmt.Sprintf("Private key: %064x\nWIF: %s\nEndereço: %s\n", privKey, wifKey, address))
	if err != nil {
		fmt.Printf("Erro ao escrever chave encontrada: %v\n", err)
	}
}


func main() {
    ranges, err := LoadRangesFromJSON("ranges.json")
    if err != nil {
        fmt.Printf("Error loading ranges: %v\n", err)
        return
    }

    for i, r := range ranges {
        // Recria o canal done para cada nova busca
        done = make(chan struct{})
        
        fmt.Printf("\nPING PONG range %d:\n", i+1)
        Search(r)
    }
}
