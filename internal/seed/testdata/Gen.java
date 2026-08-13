import java.util.Random;

// Generates golden vectors from the real java.util.Random, which is what
// Minecraft's LegacyRandomSource is bit-compatible with.
public class Gen {
    public static void main(String[] args) {
        long[] seeds = {0L, 1L, -1L, 123456789L, -987654321L, Long.MIN_VALUE, Long.MAX_VALUE};
        int[] bounds = {1, 2, 16, 17, 24, 31, 32, 1000};

        System.out.println("# nextInt: seed bound v0,v1,...,v7");
        for (long seed : seeds) {
            for (int bound : bounds) {
                Random random = new Random(seed);
                StringBuilder out = new StringBuilder();
                for (int i = 0; i < 8; i++) {
                    if (i > 0) out.append(',');
                    out.append(random.nextInt(bound));
                }
                System.out.println(seed + " " + bound + " " + out);
            }
        }

        // Structure placement, reproducing
        // RandomSpreadStructurePlacement.getPotentialStructureChunk exactly as
        // disassembled from the 26.2 jar.
        System.out.println("# placement: seed spacing separation salt spread chunkX chunkZ -> posX posZ");
        long[] placementSeeds = {0L, 1L, -1L, 987654321L, -123456789L};
        int[][] configs = {{32, 8, 14357617, 0}, {32, 8, 14357617, 1}, {34, 8, 0, 0}, {1, 0, 12345, 0}};
        int[][] chunks = {{0, 0}, {5, 5}, {-1, -1}, {-33, 40}, {1000, -1000}};
        for (long seed : placementSeeds) {
            for (int[] config : configs) {
                for (int[] chunk : chunks) {
                    int spacing = config[0], separation = config[1], salt = config[2], spread = config[3];
                    int regionX = Math.floorDiv(chunk[0], spacing);
                    int regionZ = Math.floorDiv(chunk[1], spacing);
                    long value = (long) regionX * 341873128712L + (long) regionZ * 132897987541L + seed + salt;
                    Random random = new Random(value);
                    int bound = spacing - separation;
                    int offsetX = evaluate(random, bound, spread);
                    int offsetZ = evaluate(random, bound, spread);
                    System.out.println(seed + " " + spacing + " " + separation + " " + salt + " " + spread
                            + " " + chunk[0] + " " + chunk[1] + " "
                            + (regionX * spacing + offsetX) + " " + (regionZ * spacing + offsetZ));
                }
            }
        }
    }

    // spread 0 = LINEAR, 1 = TRIANGULAR
    private static int evaluate(Random random, int bound, int spread) {
        if (spread == 1) {
            return (random.nextInt(bound) + random.nextInt(bound)) / 2;
        }
        return random.nextInt(bound);
    }
}
