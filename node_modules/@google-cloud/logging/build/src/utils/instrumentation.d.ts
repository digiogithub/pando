/*!
 * Copyright 2022 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
import { Entry } from '../entry';
export declare const DIAGNOSTIC_INFO_KEY = "logging.googleapis.com/diagnostic";
export declare const INSTRUMENTATION_SOURCE_KEY = "instrumentation_source";
export declare const NODEJS_LIBRARY_NAME_PREFIX = "nodejs";
/**
 * Default library version to be used
 * Using release-please annotations to update DEFAULT_INSTRUMENTATION_VERSION with latest version.
 * See https://github.com/googleapis/release-please/blob/main/docs/customizing.md#updating-arbitrary-files
 */
export declare const NODEJS_DEFAULT_LIBRARY_VERSION = "11.2.1";
export declare const MAX_INSTRUMENTATION_COUNT = 3;
export type InstrumentationInfo = {
    name: string;
    version: string;
};
/**
 * This method helps to populate entries with instrumentation data
 * @param entry {Entry} The entry or array of entries to be populated with instrumentation info
 * @returns [Entry[], boolean] Array of entries which contains an entry with current library
 * instrumentation info and boolean flag indicating if instrumentation was added or not in this call
 */
export declare function populateInstrumentationInfo(entry: Entry | Entry[]): [Entry[], boolean];
/**
 * The helper method to generate a log entry with diagnostic instrumentation data.
 * @param libraryName {string} The name of the logging library to be reported. Should be prefixed with 'nodejs'.
 *        Will be truncated if longer than 14 characters.
 * @param libraryVersion {string} The version of the logging library to be reported. Will be truncated if longer than 14 characters.
 * @returns {Entry} The entry with diagnostic instrumentation data.
 */
export declare function createDiagnosticEntry(libraryName: string | undefined, libraryVersion: string | undefined): Entry;
/**
 * The helper function to retrieve current library version from annotated NODEJS_DEFAULT_LIBRARY_VERSION
 * @returns {string} A current library version.
 */
export declare function getNodejsLibraryVersion(): string;
/**
 * The helper method used to set a status of a flag which indicates if instrumentation info already written or not.
 * @param value {boolean} The value to be set.
 * @returns The value of the flag before it is set.
 */
export declare function setInstrumentationStatus(value: boolean): any;
