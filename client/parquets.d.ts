declare module 'parquets' {
    export class ParquetWriter {
      static openFile(schema: any, path: string): Promise<ParquetWriter>;
      appendRow(row: any): Promise<void>;
      close(): Promise<void>;
      getBuffer(): ArrayBuffer;
    }
  }