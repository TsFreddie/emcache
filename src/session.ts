const SESSIONS: Session[] = [];

export abstract class Session {
  // the session type
  // - primary: watching & caching
  // - secondary: fake download
  // - passthrough: watching & passthrough
  private type: "primary" | "secondary" | "passthrough";
  constructor(type: typeof this.type) {
    this.type = type;
  }
}

export class PrimarySession extends Session {
  private url: string;

  constructor(url: string) {
    super("primary");

    this.url = url;
  }
}


/**
 * /emby/Items/925004/Download?X-Emby-Device-Id=A69989EE-F580-4D9C-AC02-62D2826CE5F5&api_key=f681aeb73d1d476eaba79dc0c2f9b71d&X-Emby-Device-Name=Android&X-Emby-Token=f681aeb73d1d476eaba79dc0c2f9b71d&X-Emby-Client-Version=2.0.2&X-Emby-Client=VidHub&mediaSourceId=mediasource_925004
 * 
 * 
 */
// export class DownloadSession extends Session {
//   private mediaSourceId: string;
//   private apiKey: string;
//   private 
// }