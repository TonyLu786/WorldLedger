package bundle

import "testing"

func FuzzParseManifest(f *testing.F) {
	seeds := [][]byte{
		[]byte(`{
			"schema":"worldledger.capture-bundle/v1",
			"server_id":"example.org:25565",
			"dimension":"minecraft:overworld",
			"chunk":{"x":0,"z":0},
			"observed_at":"2026-08-09T12:00:00Z",
			"protocol":"minecraft-java/26.2;canonical=worldledger.minecraft.java.chunk/v1",
			"source":{"contributor":"fuzz-seed"},
			"components":{"mcjava.shape":{
				"path":"components/shape.bin",
				"algorithm":"sha256",
				"digest":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				"size":0
			}}
		}`),
		[]byte(`{"schema":"worldledger.capture-bundle/v1","schema":"duplicate"}`),
		[]byte(`[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]`),
		{0xff, 0xfe, 0xfd},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	limits := DefaultLimits()
	f.Fuzz(func(t *testing.T, data []byte) {
		if int64(len(data)) > limits.MaxManifestBytes {
			data = data[:limits.MaxManifestBytes]
		}
		_, _ = parseManifest(data, limits)
	})
}
