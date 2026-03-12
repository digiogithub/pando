import { TimeSeries } from './types';
/** Returns the minimum number of arrays of max size chunkSize, partitioned from the given array. */
export declare function partitionList(list: TimeSeries[], chunkSize: number): TimeSeries[][];
/** Mounts the GCP project id path */
export declare function mountProjectIdPath(projectId: string): string;
/**
 * Returns the result of 2^value
 */
export declare function exp2(value: number): number;
/**
 * Map array of numbers to strings
 *
 * @param values an array of numbers
 * @returns a list of strings for those integers
 */
export declare function numbersToStrings(values: number[]): string[];
