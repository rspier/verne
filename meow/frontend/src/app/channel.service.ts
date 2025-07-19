import { Injectable } from '@angular/core';
import { BehaviorSubject } from 'rxjs';

@Injectable({
  providedIn: 'root'
})
export class ChannelService {
  private activeChannelSubject = new BehaviorSubject<any>(null);
  activeChannel$ = this.activeChannelSubject.asObservable();

  constructor() { }

  setActiveChannel(channel: any) {
    this.activeChannelSubject.next(channel);
  }
}
