import { Component, OnInit } from '@angular/core';
import { SseService } from '../sse.service';

@Component({
  selector: 'app-chat',
  templateUrl: './chat.html',
  styleUrls: ['./chat.css']
})
export class ChatComponent implements OnInit {
  messages: any[] = [];
  user = {
    id: 1,
    name: 'Test User',
    avatar_url: 'https://i.pravatar.cc/40'
  };

  constructor(private sseService: SseService) { }

  ngOnInit(): void {
    this.sseService.getServerSentEvent('http://localhost:8080/events')
      .subscribe(data => {
        this.messages.push({
          content: data,
          user: this.user,
          timestamp: new Date()
        });
      });
  }
}
