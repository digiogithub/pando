/**
 * A specialized LRU map that enforces limits on both the number of entries and
 * the total character length of the keys.
 *
 * When adding a new item, if the new key's length would exceed `maxDataSize`,
 * it evicts the least recently used items until space is available.
 *
 * @template V - The type of the values stored in the map.
 */
export declare class DataLimitedLruMap<V> {
    private readonly map;
    private readonly maxDataSize;
    private currentDataSize;
    constructor(maxKeys: number, maxDataSize: number);
    get(key: string): V | undefined;
    set(key: string, value: V): void;
    get size(): number;
    get currentDataSizeValue(): number;
}
