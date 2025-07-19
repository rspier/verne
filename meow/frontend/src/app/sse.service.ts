import { Injectable, NgZone } from '@angular/core';
import { Observable } from 'rxjs';

@Injectable({
  providedIn: 'root'
})
export class SseService {
  constructor(private zone: NgZone) {}

  getServerSentEvent(url: string): Observable<any> {
    return new Observable(observer => {
      const eventSource = new EventSource(url);
      eventSource.onmessage = event => {
        this.zone.run(() => {
          observer.next(event.data);
        });
      };
      eventSource.onerror = error => {
        this.zone.run(() => {
          observer.error(error);
        });
      };
      return () => {
        eventSource.close();
      };
    });
  }
}
