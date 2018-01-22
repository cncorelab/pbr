package pbr

import (
	"math"
	"math/rand"
	"time"

	"github.com/hunterloftis/pbr/geom"
	"github.com/hunterloftis/pbr/rgb"
)

const sampleSize = 256

type sampler struct {
	bounces int
	direct  int
	branch  int
	camera  *Camera
	scene   *Scene
}

type sample struct {
	index  uint
	energy rgb.Energy
}

func (s *sampler) start(in <-chan *[sampleSize]uint, out chan<- *[sampleSize]sample) {
	width := uint(s.camera.Width()) // TODO: change all these uints back to ints except where necessary
	height := uint(s.camera.Height())
	size := width * height
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	buffer := &[sampleSize]sample{}
	go func() {
		for pixels := range in {
			for i, pixel := range pixels {
				p := pixel % size
				x, y := float64(p%width), float64(p/width)
				e := s.tracePrimary(x, y, rnd)
				buffer[i] = sample{p, e}
			}
			out <- buffer
		}
	}()
}

func (s *sampler) tracePrimary(x, y float64, rnd *rand.Rand) (energy rgb.Energy) {
	ray := s.camera.ray(x, y, rnd)
	hit := s.scene.Intersect(ray)
	if !hit.Ok {
		return s.scene.EnvAt(ray.Dir)
	}
	point := ray.Moved(hit.Dist)
	normal, mat := hit.Surface.At(point)
	energy = energy.Plus(mat.Emit())
	branch := 1 + int(float64(s.branch)*(mat.Roughness()+0.25)/1.25)
	sum := rgb.Energy{}
	lights := s.scene.Lights()
	for i := 0; i < branch; i++ {
		dir, signal, diffused := mat.Bsdf(normal, ray.Dir, hit.Dist, rnd)
		if diffused && lights > 0 {
			direct, coverage := s.traceDirect(lights, point, normal, rnd)
			sum = sum.Plus(direct.Strength(mat.Color()))
			signal = signal.Amplified(1 - coverage)
		}
		next := geom.NewRay(point, dir)
		sum = sum.Plus(s.traceIndirect(next, 1, signal, rnd))
	}
	average := sum.Amplified(1 / float64(branch))
	return energy.Plus(average)
}

func (s *sampler) traceIndirect(ray *geom.Ray3, depth int, signal rgb.Energy, rnd *rand.Rand) (energy rgb.Energy) {
	if depth >= s.bounces {
		return energy
	}
	if signal = signal.RandomGain(rnd); signal.Zero() {
		return energy
	}
	hit := s.scene.Intersect(ray)
	if !hit.Ok {
		energy = energy.Merged(s.scene.EnvAt(ray.Dir), signal)
		return energy
	}
	point := ray.Moved(hit.Dist)
	normal, mat := hit.Surface.At(point)
	energy = energy.Merged(mat.Emit(), signal)
	dir, strength, diffused := mat.Bsdf(normal, ray.Dir, hit.Dist, rnd)
	lights := s.scene.Lights()
	if diffused && lights > 0 {
		direct, coverage := s.traceDirect(lights, point, normal, rnd)
		energy = energy.Merged(direct.Strength(mat.Color()), signal)
		signal = signal.Amplified(1 - coverage)
	}
	next := geom.NewRay(point, dir)
	return energy.Plus(s.traceIndirect(next, depth+1, signal.Strength(strength), rnd))
}

func (s *sampler) traceDirect(num int, point geom.Vector3, normal geom.Direction, rnd *rand.Rand) (energy rgb.Energy, coverage float64) {
	limit := int(math.Min(float64(s.direct), float64(num)))
	for i := 0; i < limit; i++ {
		light := s.scene.Light(rnd)
		shadow, solidAngle := light.Box().ShadowRay(point, rnd)
		cos := shadow.Dir.Cos(normal)
		if cos <= 0 {
			break
		}
		coverage += solidAngle
		hit := s.scene.Intersect(shadow)
		if !hit.Ok {
			break
		}
		e := hit.Surface.Material().Emit().Amplified(solidAngle * cos / math.Pi)
		energy = energy.Plus(e)
	}
	return energy, coverage
}