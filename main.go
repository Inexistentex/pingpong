package main

import (
    "bytes"
    "crypto/rand"
    "encoding/json"
    "fmt"
    "math/big"
    "os"
    "runtime"
    "strings"
    "sync"
    "sync/atomic"
    "time"
    "btchunt/wif"
)

const (
    minBallSize     = 50000     // Aumentado para cobrir mais espaço por verificação
    maxBallSize     = 1000000   // Mantido alto para permitir saltos maiores
    defaultBallSize = 100000    // Aumentado para melhor cobertura inicial
)

const (
    initialVelocity     = 0.5    // Aumentado para cobrir mais espaço rapidamente
    baseEnergyLoss      = 0.02   // Reduzido para manter momentum por mais tempo
    adaptiveEnergyLoss  = 0.008  // Reduzido para ajustes mais suaves
    minVelocity         = 0.005  // Aumentado para evitar busca muito localizada
    maxVelocity         = 0.7    // Aumentado para permitir saltos maiores
    momentumFactor      = 0.25   // Aumentado para maior persistência na direção
    adaptiveRangeFactor = 0.2    // Aumentado para adaptação mais rápida
)

// Adicione estas novas estruturas e variáveis
type ProgressBar struct {
    totalSize   *big.Int
    segments    int
    hits        []uint64    // Corrigido para uint64
    maxHits     uint64
    mu          sync.Mutex
}

var base58Alphabet = []byte("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")

const progressBarWidth = 100 // Largura da barra de progresso

func NewProgressBar(min, max *big.Int) *ProgressBar {
    return &ProgressBar{
        totalSize: new(big.Int).Sub(max, min),
        segments:  progressBarWidth,
        hits:      make([]uint64, progressBarWidth), // Corrigido para uint64
        maxHits:   0,
    }
}

// RangeConfig define a estrutura do arquivo JSON
type RangeConfig struct {
    Ranges []struct {
        Min    string `json:"min"`
        Max    string `json:"max"`
        Status string `json:"status"`
    } `json:"ranges"`
}

func (pb *ProgressBar) updateHit(position *big.Int, rangeMin *big.Int) {
    pb.mu.Lock()
    defer pb.mu.Unlock()

    // Calcula a posição relativa no range
    relativePos := new(big.Int).Sub(position, rangeMin)
    totalSize := pb.totalSize
    
    // Calcula o índice do segmento
    segmentFloat := new(big.Float).Quo(
        new(big.Float).SetInt(relativePos),
        new(big.Float).SetInt(totalSize),
    )
    segmentFloat = new(big.Float).Mul(segmentFloat, new(big.Float).SetInt64(int64(pb.segments)))
    
    segment, _ := segmentFloat.Int64()
    
    // Garante que o índice está dentro dos limites
    if segment >= 0 && segment < int64(pb.segments) {
        // Incrementa o contador de hits para este segmento
        pb.hits[segment]++
        
        // Atualiza o máximo de hits se necessário
        if pb.hits[segment] > pb.maxHits {
            pb.maxHits = pb.hits[segment]
        }
    }
}


func (pb *ProgressBar) render() string {
    pb.mu.Lock()
    defer pb.mu.Unlock()

    var buffer strings.Builder
    buffer.WriteString("[")

    // Ajusta os limiares para uma progressão mais gradual
    for _, hits := range pb.hits {
        if hits == 0 {
            buffer.WriteString(" ")
            continue
        }

        // Calcula a proporção de hits em relação ao máximo
        ratio := float64(hits) / float64(pb.maxHits)
        
        // Novos limiares mais graduais
        switch {
        case ratio < 0.25:
            buffer.WriteString("░") // Muito baixo
        case ratio < 0.50:
            buffer.WriteString("▒") // Baixo
        case ratio < 0.75:
            buffer.WriteString("▓") // Médio
        default:
            buffer.WriteString("█") // Alto
        }
    }
    
    buffer.WriteString("]")
    return buffer.String()
}


// Range defines the search interval
type Range struct {
    Min    *big.Int
    Max    *big.Int
    Status []byte // Modificado para armazenar diretamente o hash160
}

// Stats mantém as estatísticas da busca
type Stats struct {
    keysChecked     uint64
    startTime       time.Time
    lastSpeedUpdate time.Time
    progressBar     *ProgressBar
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

        // Converte o endereço diretamente para hash160
        hash160 := addressToHash160(r.Status)

        ranges[i] = Range{
            Min:    min,
            Max:    max,
            Status: hash160,
        }
    }

    return ranges, nil
}

// PrintProgress imprime as estatísticas de progresso
func PrintProgress(stats *Stats) {
    // Limpa a tela
    fmt.Print("\033[2J\033[H")
    
    // Imprime o cabeçalho inicial uma única vez
    header := fmt.Sprintf("Searching range 1:\n" +
        "Target hash160: 2f396b29b27324300d0c59b17c3abc1835bd3dbb\n" +
        "Target Address Hash160: 2f396b29b27324300d0c59b17c3abc1835bd3dbb\n" +
        "Range Min (hex): 1000000\n" +
        "Range Max (hex): 1ffffff\n" +
        "Range Size (keys): 16777216\n" +
        "Using %d CPU cores\n", runtime.NumCPU())
    
    fmt.Print(header)
    
    // Posição para começar a atualizar as estatísticas
    basePosition := 8
    
    for {
        time.Sleep(time.Second)
        
        // Move o cursor para a posição base e limpa até o final da tela
        fmt.Printf("\033[%d;0H\033[J", basePosition)
        
        keysChecked := atomic.LoadUint64(&stats.keysChecked)
        duration := time.Since(stats.startTime)
        keysPerSecond := float64(keysChecked) / duration.Seconds()
        
        // Atualiza as estatísticas
        fmt.Printf("Keys Checked: %d | Keys/sec: %.2f | Time Elapsed: %s\n", 
            keysChecked, 
            keysPerSecond, 
            duration.Round(time.Second))
        
        // Atualiza a barra de progresso
        fmt.Printf("Progress: %s\n", stats.progressBar.render())
        fmt.Printf("Legend: ░ (<25%%) ▒ (<50%%) ▓ (<75%%) █ (>75%%)")
    }
}

func shouldSkipKey(key *big.Int) bool {
    hexKey := key.Text(16)
    
    if len(hexKey) < 4 {
        return false
    }

    count := 1
    lastChar := hexKey[0]

    for i := 1; i < len(hexKey); i++ {
        currentChar := hexKey[i]
        if currentChar == lastChar && currentChar != '0' {
            count++
            if count >= 4 {
                return true
            }
        } else {
            count = 1
        }
        lastChar = currentChar
    }

    return false
}

// CheckAddressWithWIF verifica se uma chave privada corresponde ao hash160 alvo
func CheckAddressWithWIF(privateKey *big.Int, targetHash160 []byte) bool {
    privKeyBytes := privateKey.FillBytes(make([]byte, 32))
    pubKey := wif.GeneratePublicKey(privKeyBytes)
    hash160 := wif.Hash160(pubKey)
    return bytes.Equal(hash160, targetHash160)
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
    
    ball := new(big.Int).Set(startPoint)
    rangeSize := new(big.Int).Sub(r.Max, r.Min)
    currentKey := new(big.Int)

    for {
        select {
        case <-found:
            return
        default:
            jump := GenerateSmartJump(rangeSize, strategy)
            
            if strategy.direction > 0 {
                ball.Add(ball, jump)
                if ball.Cmp(r.Max) > 0 {
                    ball.Sub(r.Max, new(big.Int).Sub(ball, r.Max))
                    strategy.direction *= -1
                    atomic.AddUint64(&strategy.bounceCount, 1)
                }
            } else {
                ball.Sub(ball, jump)
                if ball.Cmp(r.Min) < 0 {
                    ball.Add(r.Min, new(big.Int).Sub(r.Min, ball))
                    strategy.direction *= -1
                    atomic.AddUint64(&strategy.bounceCount, 1)
                }
            }

            stats.progressBar.updateHit(ball, r.Min)

            currentKey.Set(ball)
            for i := int64(0); i < strategy.ballRange.size; i++ {
                if currentKey.Cmp(r.Max) > 0 || currentKey.Cmp(r.Min) < 0 {
                    break
                }

                atomic.AddUint64(&stats.keysChecked, 1)

                if !shouldSkipKey(currentKey) {
                    if CheckAddressWithWIF(currentKey, r.Status) {
                        found <- new(big.Int).Set(currentKey)
                        return
                    }
                }

                currentKey.Add(currentKey, big.NewInt(1))
            }
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

    velocityAdjustment := 1.0 - (localDensity * 0.1)
    if velocityAdjustment < 0.1 {
        velocityAdjustment = 0.1
    }

    targetVelocity := js.velocity * velocityAdjustment
    js.momentum = js.momentum*momentumFactor + targetVelocity*(1-momentumFactor)
    
    if js.momentum < minVelocity {
        js.momentum = minVelocity
    } else if js.momentum > maxVelocity {
        js.momentum = maxVelocity
    }

    rangeSizeFloat := new(big.Float).SetInt(rangeSize)
    jumpSize := new(big.Float).Mul(rangeSizeFloat, big.NewFloat(js.momentum))

    randomness, _ := rand.Int(rand.Reader, big.NewInt(100))
    randomFactor := 1.0 + (float64(randomness.Int64()%50) - 25.0) / 1000.0
    jumpSize = new(big.Float).Mul(jumpSize, big.NewFloat(randomFactor))

    jumpInt, _ := jumpSize.Int(nil)
    ballRangeSize := big.NewInt(js.ballRange.size)
    jumpInt.Div(jumpInt, ballRangeSize)
    jumpInt.Mul(jumpInt, ballRangeSize)

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
        momentum:      0.0,
        successRate:   1.0,
        lastJump:      new(big.Int),
        lastPositions: lastPositions,
        coverage:      make(map[string]uint64),
        ballRange: BallRange{
            current: new(big.Int).Set(initialPosition),
            size:    10000,
        },
    }
}

// Search implementa a busca principal
func Search(r Range) {
    numCPU := runtime.NumCPU()
    found := make(chan *big.Int, 1)
    var wg sync.WaitGroup
    
    progressBar := NewProgressBar(r.Min, r.Max)
    stats := &Stats{
        startTime: time.Now(),
        lastSpeedUpdate: time.Now(),
        progressBar: progressBar,
    }

    rangeSize := new(big.Int).Sub(r.Max, r.Min)
    
    fmt.Print("\033[H\033[2J")
    fmt.Printf("Range Min (hex): %s\n", r.Min.Text(16))
    fmt.Printf("Range Max (hex): %s\n", r.Max.Text(16))

    keyCount := new(big.Int).Add(
        new(big.Int).Sub(r.Max, r.Min),
        big.NewInt(1),
    )
    fmt.Printf("Range Size (keys): %s\n", keyCount.Text(10))
    
    fmt.Printf("Using %d CPU cores\n\n", numCPU)

    go PrintProgress(stats)

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

    foundKey := <-found
    privKeyBytes := foundKey.FillBytes(make([]byte, 32))
    pubKey := wif.GeneratePublicKey(privKeyBytes)
    wifKey := wif.PrivateKeyToWIF(foundKey)
    address := wif.PublicKeyToAddress(pubKey)
    
    // Chama a função saveFoundKeyDetails quando a chave é encontrada
    saveFoundKeyDetails(foundKey, wifKey, address)

    close(found)
    wg.Wait()
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
        fmt.Printf("\nSearching range %d:\n", i+1)
        fmt.Printf("Min: %s\n", r.Min.Text(16))
        fmt.Printf("Max: %s\n", r.Max.Text(16))
        fmt.Printf("Target hash160: %x\n\n", r.Status)
        
        Search(r)
    }
}
