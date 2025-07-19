import { Component, OnInit } from '@angular/core';
import { SseService } from '../sse.service';
import { QuillModule } from 'ngx-quill';
import { CommonModule } from '@angular/common';
import { MessageComponent } from '../message/message';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { NotificationService } from '../notification.service';
import { ChannelService } from '../channel.service';

@Component({
  selector: 'app-chat',
  templateUrl: './chat.html',
  styleUrls: ['./chat.css'],
  standalone: true,
  imports: [QuillModule, CommonModule, MessageComponent, FormsModule]
})
export class ChatComponent implements OnInit {
  messages: any[] = [];
  user = {
    id: 1,
    name: 'Test User',
    avatar_url: 'https://i.pravatar.cc/40'
  };
  messageContent = '';
  quillConfig = {
    toolbar: [
      ['bold', 'italic', 'underline', 'strike'],
    ]
  };
  channels = [
    { id: 1, name: 'general' },
    { id: 2, name: 'random' }
  ];
  dms = [
    { id: 3, name: 'Alice' },
    { id: 4, name: 'Bob' }
  ];
  activeChannel: any;

  constructor(
    private sseService: SseService,
    private http: HttpClient,
    private notificationService: NotificationService,
    private channelService: ChannelService
  ) { }

  ngOnInit(): void {
    this.notificationService.requestPermission();
    this.channelService.activeChannel$.subscribe(channel => {
      this.activeChannel = channel;
      // TODO: Fetch messages for the active channel
    });
    this.channelService.setActiveChannel(this.channels[0]);
  }

  sendMessage() {
    // TODO: Implement send message functionality
    console.log(this.messageContent);
  }

  onFileSelected(event: any) {
    const file: File = event.target.files[0];
    if (file) {
      const formData = new FormData();
      formData.append('image', file);
      this.http.post('http://localhost:8080/upload', formData).subscribe(response => {
        console.log(response);
      });
    }
  }

  switchChannel(channel: any) {
    this.channelService.setActiveChannel(channel);
  }
}
